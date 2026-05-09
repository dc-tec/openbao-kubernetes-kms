//go:build unix

package socket_test

import (
	"os"
	"syscall"
)

type unixStat struct {
	uid uint32
	gid uint32
}

func sysStat(info os.FileInfo) (unixStat, bool) {
	raw, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return unixStat{}, false
	}
	return unixStat{uid: raw.Uid, gid: raw.Gid}, true
}
