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
	// unix.SockFprog/SockFilter are generated per-GOARCH so the pointer
	// alignment matches the kernel's struct sock_fprog on 32- and 64-bit
	// Linux. A hand-rolled struct would mis-align on 32-bit and prctl
	// would silently return EINVAL.
	filter := make([]unix.SockFilter, len(raw))
	for i, r := range raw {
		filter[i] = unix.SockFilter{Code: r.Op, Jt: r.Jt, Jf: r.Jf, K: r.K}
	}
	// #nosec G115 -- sock_fprog.len is uint16; Linux's BPF_MAXINSNS caps a
	// BPF program at 4096 instructions long before len(raw) could overflow
	// uint16, and our filter is 11 instructions. The cast is safe.
	fprog := unix.SockFprog{
		Len:    uint16(len(filter)),
		Filter: &filter[0],
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
	// unix.Write is a raw write(2) wrapper that can return a short write
	// without an error. The BPF filter is small (~176 bytes) and the memfd
	// is RAM-backed, so a partial write is extremely unlikely — but the
	// write(2) contract requires looping until done, and a truncated BPF
	// program reaches bwrap and is rejected by the kernel with no clear
	// indication of where the truncation happened.
	for off := 0; off < len(buf); {
		n, err := unix.Write(memfd, buf[off:])
		if err != nil {
			_ = unix.Close(memfd)
			return nil, fmt.Errorf("write memfd: %w", err)
		}
		if n <= 0 {
			_ = unix.Close(memfd)
			return nil, fmt.Errorf("write memfd: non-positive write of %d bytes at offset %d", n, off)
		}
		off += n
	}
	if _, err := unix.Seek(memfd, 0, 0); err != nil {
		_ = unix.Close(memfd)
		return nil, fmt.Errorf("seek memfd: %w", err)
	}
	f := os.NewFile(uintptr(memfd), "aide-seccomp-bpf")
	// Pin against GC so the launcher's runtime finalizer cannot close the fd
	// after applyBwrap returns and cmd is dropped. Symmetric to the policy
	// memfd in writePolicyToMemfd; same EBADF-in-child symptom otherwise.
	// (runtime.SetFinalizer(f, nil) does not work — finalizer is on the
	// unexported f.file inner struct.)
	pinSandboxMemfd(f)
	return f, nil
}
