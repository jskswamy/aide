//go:build linux

package sandbox

import (
	"encoding/binary"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/net/bpf"
	"golang.org/x/sys/unix"
)

// Audit architecture identifiers embedded in seccomp_data.arch.
// The filter checks this field first so that the syscall-number comparisons
// that follow are interpreted against the correct ABI (numbers differ between
// x86_64 and arm64; arm64 has no separate fork/vfork syscalls at all).
const (
	auditArchX86_64  uint32 = 0xC000003E
	auditArchAARCH64 uint32 = 0xC00000B7
)

// Syscall numbers used by the no-subprocess BPF filter, spelled out as
// literals so the file compiles on all Linux architectures without pulling
// in arch-gated unix.SYS_* constants (e.g. unix.SYS_FORK is undefined on
// arm64).
const (
	// x86_64
	sysX86_64Fork   uint32 = 57
	sysX86_64VFork  uint32 = 58
	sysX86_64Clone  uint32 = 56
	sysX86_64Clone3 uint32 = 435

	// arm64 — no separate fork/vfork; all process/thread creation uses clone.
	sysARM64Clone  uint32 = 220
	sysARM64Clone3 uint32 = 435
)

// seccompRetAllow / seccompRetErrnoEPERM / seccompRetErrnoENOSYS are the
// action values returned by the BPF program. SECCOMP_RET_ERRNO occupies the
// high 16 bits; the errno number is in the low 16 bits.
//
// clone3 must return ENOSYS (not EPERM): glibc 2.34+ (Ubuntu 22.04+) uses
// clone3 inside pthread_create and only falls back to clone() when clone3
// returns ENOSYS. An EPERM response propagates directly to the caller, making
// pthread_create fail — which crashes any Node.js/libuv agent that creates a
// thread pool at startup (e.g. Claude on Electron).
const (
	seccompRetAllow       uint32 = 0x7FFF0000
	seccompRetErrnoEPERM  uint32 = 0x00050001
	seccompRetErrnoENOSYS uint32 = 0x00050026
)

// sockFprog mirrors the Linux struct sock_fprog used by prctl(PR_SET_SECCOMP,
// SECCOMP_MODE_FILTER, &fprog).
type sockFprog struct {
	Len    uint16
	pad    [6]byte // align to 8 bytes for 64-bit pointer alignment
	Filter *bpf.RawInstruction
}

// buildNoSubprocessFilter returns a seccomp BPF program that blocks process
// creation on both x86_64 and arm64. The filter dispatches on the audit arch
// field first so syscall-number comparisons are always against the correct ABI.
// Unknown architectures are passed through (ALLOW) — subprocess creation is
// unblocked on those hosts; the PID namespace created by forkExecInPIDNamespace
// (Landlock path) still provides containment, but does not prevent fork/clone.
//
// clone3 returns ENOSYS (not EPERM) on both architectures so that glibc 2.34+
// (Ubuntu 22.04+) falls back to clone() for pthread_create. With EPERM, glibc
// propagates the error directly to the caller, crashing any agent that creates
// a thread pool at start-up (Node.js/libuv, Electron). ENOSYS triggers the
// standard "syscall not implemented" fallback path in glibc.
//
// Filter pseudocode:
//
//	load arch
//	if arch == x86_64:  goto x86_64_block          (instr 12)
//	if arch == aarch64: goto arm64_block            (instr 4)
//	ALLOW                                           (unknown arch)
//
//	arm64_block (instrs 4–11):
//	  load nr
//	  if nr == clone3:                 return ENOSYS (instr 10)
//	  if nr != clone:                  ALLOW         (instr 11)
//	  load flags (args[0] low 32)
//	  if flags & CLONE_THREAD:         ALLOW         (instr 11)
//	  return EPERM                                   (instr 9)
//	  return ENOSYS                                  (instr 10)
//	  ALLOW                                          (instr 11)
//
//	x86_64_block (instrs 12–21):
//	  load nr
//	  if nr == fork|vfork:             return EPERM  (instr 19)
//	  if nr == clone3:                 return ENOSYS (instr 20)
//	  if nr != clone:                  ALLOW         (instr 21)
//	  load flags (args[0] low 32)
//	  if flags & CLONE_THREAD:         ALLOW         (instr 21)
//	  return EPERM                                   (instr 19)
//	  return ENOSYS                                  (instr 20)
//	  ALLOW                                          (instr 21)
func buildNoSubprocessFilter() ([]bpf.RawInstruction, error) {
	insts := []bpf.Instruction{
		// 0: load arch (seccomp_data.arch at offset 4)
		bpf.LoadAbsolute{Off: 4, Size: 4},
		// 1: if arch == x86_64, skip 10 → land at instr 12 (x86_64 block)
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: auditArchX86_64, SkipTrue: 10, SkipFalse: 0},
		// 2: if arch == aarch64, skip 1 → land at instr 4 (arm64 block); else fall to 3
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: auditArchAARCH64, SkipTrue: 1, SkipFalse: 0},
		// 3: unknown arch — ALLOW
		bpf.RetConstant{Val: seccompRetAllow},

		// arm64 block ────────────────────────────────────────────────────────
		// 4: load nr (seccomp_data.nr at offset 0)
		bpf.LoadAbsolute{Off: 0, Size: 4},
		// 5: if nr == clone3(435), skip 4 → ENOSYS at instr 10
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: sysARM64Clone3, SkipTrue: 4, SkipFalse: 0},
		// 6: if nr != clone(220), skip 4 → ALLOW at instr 11
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: sysARM64Clone, SkipTrue: 0, SkipFalse: 4},
		// 7: load args[0] low 32 (clone flags; little-endian, low half at offset 16)
		bpf.LoadAbsolute{Off: 16, Size: 4},
		// 8: if CLONE_THREAD set, skip 2 → ALLOW at instr 11; else fall to EPERM
		bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: unix.CLONE_THREAD, SkipTrue: 2, SkipFalse: 0},
		// 9: EPERM (clone without CLONE_THREAD → subprocess blocked)
		bpf.RetConstant{Val: seccompRetErrnoEPERM},
		// 10: ENOSYS (clone3 on arm64 → glibc fallback to clone())
		bpf.RetConstant{Val: seccompRetErrnoENOSYS},
		// 11: ALLOW
		bpf.RetConstant{Val: seccompRetAllow},

		// x86_64 block ───────────────────────────────────────────────────────
		// 12: load nr
		bpf.LoadAbsolute{Off: 0, Size: 4},
		// 13: if nr == fork(57), skip 5 → EPERM at instr 19
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: sysX86_64Fork, SkipTrue: 5, SkipFalse: 0},
		// 14: if nr == vfork(58), skip 4 → EPERM at instr 19
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: sysX86_64VFork, SkipTrue: 4, SkipFalse: 0},
		// 15: if nr == clone3(435), skip 4 → ENOSYS at instr 20
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: sysX86_64Clone3, SkipTrue: 4, SkipFalse: 0},
		// 16: if nr != clone(56), skip 4 → ALLOW at instr 21
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: sysX86_64Clone, SkipTrue: 0, SkipFalse: 4},
		// 17: load args[0] low 32 (clone flags)
		bpf.LoadAbsolute{Off: 16, Size: 4},
		// 18: if CLONE_THREAD set, skip 2 → ALLOW at instr 21; else fall to EPERM
		bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: unix.CLONE_THREAD, SkipTrue: 2, SkipFalse: 0},
		// 19: EPERM (fork/vfork/clone-without-thread → subprocess blocked)
		bpf.RetConstant{Val: seccompRetErrnoEPERM},
		// 20: ENOSYS (clone3 on x86_64 → glibc fallback to clone())
		bpf.RetConstant{Val: seccompRetErrnoENOSYS},
		// 21: ALLOW
		bpf.RetConstant{Val: seccompRetAllow},
	}
	raw, err := bpf.Assemble(insts)
	if err != nil {
		return nil, fmt.Errorf("assemble seccomp BPF: %w", err)
	}
	return raw, nil
}

// installNoSubprocessSeccomp installs the no-subprocess seccomp filter on the
// current process. The filter survives execve so a wrapper process can install
// it before exec'ing the agent and the agent inherits the restriction.
// PR_SET_NO_NEW_PRIVS is applied first so the install does not require
// CAP_SYS_ADMIN.
func installNoSubprocessSeccomp() error {
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl PR_SET_NO_NEW_PRIVS: %w", err)
	}
	raw, err := buildNoSubprocessFilter()
	if err != nil {
		return err
	}
	// #nosec G115 -- sock_fprog.len is uint16; Linux's BPF_MAXINSNS caps a
	// BPF program at 4096 instructions long before len(raw) could overflow
	// uint16, and our filter is 11 instructions. The cast is safe.
	fprog := sockFprog{
		Len:    uint16(len(raw)),
		Filter: &raw[0],
	}
	// #nosec G103 -- prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &fprog) is
	// the documented Linux ABI for installing a seccomp filter; the kernel
	// requires a sock_fprog pointer passed as uintptr. fprog lives on the
	// stack here so the address is stable across the syscall.
	if err := unix.Prctl(unix.PR_SET_SECCOMP, uintptr(unix.SECCOMP_MODE_FILTER),
		uintptr(unsafe.Pointer(&fprog)), 0, 0); err != nil {
		return fmt.Errorf("prctl PR_SET_SECCOMP: %w", err)
	}
	return nil
}

// noSubprocessSeccompMemfd creates an in-memory file holding the
// no-subprocess BPF program as raw sock_filter bytes, suitable for handing
// to bwrap via its `--seccomp <fd>` option. The returned *os.File owns the
// underlying memfd; pass it via cmd.ExtraFiles so the launched child
// inherits a duplicated fd, and let the parent's *os.File close on
// cmd.Wait() / GC normally.
func noSubprocessSeccompMemfd() (*os.File, error) {
	raw, err := buildNoSubprocessFilter()
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 0, len(raw)*8)
	tmp := make([]byte, 8)
	for _, ri := range raw {
		binary.LittleEndian.PutUint16(tmp[0:2], ri.Op)
		tmp[2] = ri.Jt
		tmp[3] = ri.Jf
		binary.LittleEndian.PutUint32(tmp[4:8], ri.K)
		buf = append(buf, tmp...)
	}
	memfd, err := unix.MemfdCreate("aide-seccomp-bpf", 0)
	if err != nil {
		return nil, fmt.Errorf("memfd_create: %w", err)
	}
	if _, err := unix.Write(memfd, buf); err != nil {
		_ = unix.Close(memfd)
		return nil, fmt.Errorf("write memfd: %w", err)
	}
	if _, err := unix.Seek(memfd, 0, 0); err != nil {
		_ = unix.Close(memfd)
		return nil, fmt.Errorf("seek memfd: %w", err)
	}
	return os.NewFile(uintptr(memfd), "aide-seccomp-bpf"), nil
}
