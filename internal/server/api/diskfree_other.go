//go:build !unix

package api

func diskFree(string) (uint64, bool) { return 0, false }
