//go:build !windows

package discover

import "syscall"

// RaiseFileLimit lifts the open-files limit of the process towards its hard
// limit (at most 8192). macOS starts apps with 256, fewer than the sockets a
// scan opens at the same time; port checks that cannot open a socket would
// silently miss devices.
func RaiseFileLimit() {
	var l syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &l) != nil {
		return
	}
	want := uint64(8192)
	if l.Max < want {
		want = l.Max
	}
	if l.Cur < want {
		l.Cur = want
		_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &l)
	}
}

// fileLimit is the open-files limit of the process (0 = unknown).
func fileLimit() int {
	var l syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &l) != nil || l.Cur > 1<<20 {
		return 0
	}
	return int(l.Cur)
}
