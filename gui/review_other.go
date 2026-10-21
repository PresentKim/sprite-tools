//go:build !windows

package main

import "sprite-tools/pkg/i18n"

// recycleFiles is only implemented on Windows; elsewhere files stay in the trash folders.
func recycleFiles(paths []string) error {
	return i18n.Errorf("err.gui.recycle_unsupported")
}
