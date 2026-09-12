package main

import (
	"os"
	"time"
)

// watchFile calls onChange whenever the file at path changes size or
// modification time, polling every interval. hop-writer replaces the file
// atomically, and a hand edit changes it in place; either way this notices
// within one interval. It never returns.
func watchFile(path string, interval time.Duration, onChange func()) {
	last := fileStamp(path)
	for {
		time.Sleep(interval)
		cur := fileStamp(path)
		if cur.ok && (cur.size != last.size || !cur.mod.Equal(last.mod)) {
			last = cur
			onChange()
		}
	}
}

type stamp struct {
	mod  time.Time
	size int64
	ok   bool
}

func fileStamp(path string) stamp {
	fi, err := os.Stat(path)
	if err != nil {
		return stamp{}
	}
	return stamp{mod: fi.ModTime(), size: fi.Size(), ok: true}
}
