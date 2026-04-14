// pkg/governance/locksmith.go
package governance

import (
	"fmt"
	"os"
	"syscall"
)

// LockedFile represents a file descriptor secured by the OS kernel.
type LockedFile struct {
	File *os.File
	Path string
}

// AcquireLock opens the target file and applies an exclusive kernel lock.
func AcquireLock(path string) (*LockedFile, error) {
	// Open file with Read/Write permissions. Create if it doesn't exist.
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open target file for locking: %w", err)
	}

	// Apply the exclusive kernel lock (POSIX-specific). 
	// If another process has the lock, this will block until it is released.
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to acquire syscall.Flock on %s: %w", path, err)
	}

	return &LockedFile{
		File: file,
		Path: path,
	}, nil
}

// ReleaseAndClose drops the OS lock and closes the file descriptor.
func (lf *LockedFile) ReleaseAndClose() error {
	if lf.File == nil {
		return nil
	}
	
	// Unlocking via syscall
	err := syscall.Flock(int(lf.File.Fd()), syscall.LOCK_UN)
	closeErr := lf.File.Close()

	if err != nil {
		return fmt.Errorf("failed to release flock: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close file descriptor: %w", closeErr)
	}

	return nil
}

// ProxyExecute writes the approved payload directly to the locked file descriptor.
func (lf *LockedFile) ProxyExecute(content string) error {
	// Truncate the file before writing new content
	if err := lf.File.Truncate(0); err != nil {
		return err
	}
	if _, err := lf.File.Seek(0, 0); err != nil {
		return err
	}
	
	_, err := lf.File.WriteString(content)
	return err
}