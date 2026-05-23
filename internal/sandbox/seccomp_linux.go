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

// AUDIT_ARCH_X86_64 is the audit arch identifier for x86_64. The seccomp
// filter checks the calling process's architecture so the syscall-number
// comparisons below are interpreted correctly (numbers differ on arm64,
// where SYS_FORK doesn't even exist).
const auditArchX86_64 uint32 = 0xC000003E

// seccompRetAllow / seccompRetErrnoEPERM are the action values returned by
// the BPF program for the kernel to interpret. SECCOMP_RET_ERRNO is in the
// high 16 bits; the errno number (here EPERM = 1) is in the low 16.
const (
	seccompRetAllow      uint32 = 0x7FFF0000
	seccompRetErrnoEPERM uint32 = 0x00050001
)

// sockFprog mirrors the Linux struct sock_fprog used by prctl(PR_SET_SECCOMP,
// SECCOMP_MODE_FILTER, &fprog).
type sockFprog struct {
	Len    uint16
	pad    [6]byte // align to 8 bytes for 64-bit pointer alignment
	Filter *bpf.RawInstruction
}

// buildNoSubprocessFilter returns a seccomp BPF program that blocks
// fork/vfork/clone3 outright and blocks clone() unless CLONE_THREAD is
// set in the flags argument. The filter is unconditionally permissive on
// non-x86_64 (no enforcement) — the caller should arrange for that path
// to be unreachable in practice via the build tag or a separate filter.
//
// Filter pseudocode:
//
//	if arch != AUDIT_ARCH_X86_64: ALLOW
//	if nr == fork|vfork|clone3:  return EPERM
//	if nr == clone:
//	    if flags & CLONE_THREAD: ALLOW
//	    else:                    return EPERM
//	ALLOW
func buildNoSubprocessFilter() ([]bpf.RawInstruction, error) {
	insts := []bpf.Instruction{
		// 0: load arch (seccomp_data.arch is at offset 4)
		bpf.LoadAbsolute{Off: 4, Size: 4},
		// 1: if arch != x86_64, jump to ALLOW (index 10 -> SkipFalse=8 from here)
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: auditArchX86_64, SkipTrue: 0, SkipFalse: 8},
		// 2: load nr (seccomp_data.nr at offset 0)
		bpf.LoadAbsolute{Off: 0, Size: 4},
		// 3: if nr == fork, jump to EPERM (index 9 -> SkipTrue=5)
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.SYS_FORK, SkipTrue: 5, SkipFalse: 0},
		// 4: if nr == vfork, jump to EPERM (index 9 -> SkipTrue=4)
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.SYS_VFORK, SkipTrue: 4, SkipFalse: 0},
		// 5: if nr == clone3, jump to EPERM (index 9 -> SkipTrue=3)
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.SYS_CLONE3, SkipTrue: 3, SkipFalse: 0},
		// 6: if nr != clone, jump to ALLOW (index 10 -> SkipFalse=3)
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.SYS_CLONE, SkipTrue: 0, SkipFalse: 3},
		// 7: load arg0 low 32 (seccomp_data.args[0] at offset 16; x86_64 is LE so low half is here)
		bpf.LoadAbsolute{Off: 16, Size: 4},
		// 8: if (flags & CLONE_THREAD) != 0, jump to ALLOW (index 10 -> SkipTrue=1)
		bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: unix.CLONE_THREAD, SkipTrue: 1, SkipFalse: 0},
		// 9: EPERM
		bpf.RetConstant{Val: seccompRetErrnoEPERM},
		// 10: ALLOW
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
