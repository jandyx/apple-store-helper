package checkout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
)

var contactStoreMu sync.Mutex

// DefaultContactPath returns the app's private contact location without creating it.
func DefaultContactPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil || base == "" || !filepath.IsAbs(base) {
		return "", problem("user configuration directory unavailable")
	}
	return filepath.Join(base, "apple-store-helper", "private", "pickup-contact.json"), nil
}

// ValidateContact uses the same rules as the checkout contact loader.
func ValidateContact(c Contact) error { return validatePrivateContact(c) }

// contactDirectory rejects symlink directory components and creates missing ones
// privately. Existing ancestors may be shared; the final directory must be private.
func contactDirectory(path string, create bool) error {
	parent := filepath.Dir(path)
	var walk func(string) error
	walk = func(dir string) error {
		st, err := os.Lstat(dir)
		if os.IsNotExist(err) && create {
			above := filepath.Dir(dir)
			if above == dir {
				return problem("private contact directory unavailable")
			}
			if err = walk(above); err != nil {
				return err
			}
			if err = os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
				return problem("private contact directory unavailable")
			}
			st, err = os.Lstat(dir)
		}
		if err != nil {
			return problem("private contact directory unavailable")
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return problem("unsafe private contact directory")
		}
		above := filepath.Dir(dir)
		if above != dir {
			return walk(above)
		}
		return nil
	}
	if err := walk(parent); err != nil {
		return err
	}
	st, err := os.Lstat(parent)
	if err != nil || st.Mode().Perm()&0077 != 0 {
		return problem("private contact directory permissions invalid")
	}
	return nil
}

func checkContactTarget(path string) (bool, error) {
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, problem("private contact file unavailable")
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return false, problem("unsafe private contact file")
	}
	// Nlink is present on supported Unix platforms. Fail closed elsewhere rather
	// than treating an unknown native file identity as safe.
	native := reflect.ValueOf(st.Sys())
	if native.Kind() == reflect.Pointer && !native.IsNil() {
		native = native.Elem()
	}
	if native.Kind() != reflect.Struct {
		return false, problem("private contact file identity unavailable")
	}
	links := native.FieldByName("Nlink")
	if !links.IsValid() {
		return false, problem("private contact file identity unavailable")
	}
	var count uint64
	switch links.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		count = links.Uint()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		count = uint64(links.Int())
	default:
		return false, problem("private contact file identity unavailable")
	}
	if count != 1 {
		return false, problem("linked private contact file refused")
	}
	return true, nil
}

func contactPath(path string) (string, error) {
	if runtime.GOOS == "windows" {
		return "", problem("private contact storage unsupported on Windows pending native security validation")
	}
	if path == "" {
		return "", problem("private contact path required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", problem("private contact path invalid")
	}
	return filepath.Clean(absolute), nil
}

func syncContactDirectory(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return problem("private contact directory unavailable")
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return problem("private contact directory sync failed")
	}
	return nil
}

// SaveContact atomically replaces a private contact file. It does not arm checkout.
func SaveContact(path string, c Contact) error {
	if err := ValidateContact(c); err != nil {
		return err
	}
	path, err := contactPath(path)
	if err != nil {
		return err
	}
	contactStoreMu.Lock()
	defer contactStoreMu.Unlock()
	if err = contactDirectory(path, true); err != nil {
		return err
	}
	if _, err = checkContactTarget(path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return problem("private contact encoding failed")
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".pickup-contact-*")
	if err != nil {
		return problem("private contact temporary file unavailable")
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if temp.Chmod(0600) != nil {
		return problem("private contact permissions failed")
	}
	if _, err = temp.Write(data); err != nil {
		return problem("private contact write failed")
	}
	if temp.Sync() != nil {
		return problem("private contact sync failed")
	}
	if temp.Close() != nil {
		return problem("private contact close failed")
	}
	if err = syncContactDirectory(path); err != nil {
		return err
	}
	// Recheck before replacement: never operate on an observed link or directory.
	if _, err = checkContactTarget(path); err != nil {
		return err
	}
	if err = os.Rename(temp.Name(), path); err != nil {
		return problem("private contact replacement failed")
	}
	return syncContactDirectory(path)
}

// DeleteContact deletes only the private regular contact file, never a directory.
func DeleteContact(path string) error {
	path, err := contactPath(path)
	if err != nil {
		return err
	}
	contactStoreMu.Lock()
	defer contactStoreMu.Unlock()
	exists, err := checkContactTarget(path)
	if err != nil || !exists {
		return err
	}
	if err = contactDirectory(path, false); err != nil {
		return err
	}
	if err = os.Remove(path); err != nil {
		return problem("private contact deletion failed")
	}
	return syncContactDirectory(path)
}
