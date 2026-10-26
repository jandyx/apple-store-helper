package checkout

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func contactTestDirectory(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX private storage")
	}
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(d, 0700); err != nil {
		t.Fatal(err)
	}
	return d
}
func fixtureContact() Contact {
	return Contact{FirstName: "First", LastName: "Last", Email: "test@example.test", Phone: "123456789", IDLast4: "12aX"}
}

func TestContactStoreRoundTripAndUpdate(t *testing.T) {
	d := contactTestDirectory(t)
	p := filepath.Join(d, "app", "private", "pickup-contact.json")
	c := fixtureContact()
	if err := SaveContact(p, c); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadContact(p); err != nil || got != c {
		t.Fatalf("roundtrip failed: %v", err)
	}
	for _, path := range []string{filepath.Dir(p), filepath.Dir(filepath.Dir(p))} {
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != 0700 {
			t.Fatal("directory not private")
		}
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0600 {
		t.Fatal("file not private")
	}
	c.FirstName = "Updated"
	c.IDLast4 = "" // Keep the existing optional suffix contract.
	if err := SaveContact(p, c); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadContact(p); err != nil || got != c {
		t.Fatal("update failed")
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatal("temporary files remained")
	}
}

func TestContactStoreInvalidDoesNotOverwrite(t *testing.T) {
	d := contactTestDirectory(t)
	p := filepath.Join(d, "contact.json")
	c := fixtureContact()
	if err := SaveContact(p, c); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p)
	bad := c
	bad.IDLast4 = "not-four"
	if err := SaveContact(p, bad); err == nil {
		t.Fatal("invalid accepted")
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("old contact changed")
	}
	bad = c
	bad.FirstName = ""
	if ValidateContact(bad) == nil {
		t.Fatal("validation diverged")
	}
	if err := SaveContact("", c); err == nil {
		t.Fatal("empty path accepted")
	}
}

func TestContactStoreRejectsUnsafePaths(t *testing.T) {
	d := contactTestDirectory(t)
	c := fixtureContact()
	target := filepath.Join(d, "original.json")
	if err := SaveContact(target, c); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(d, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(d, "directory")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(d, "hard.json")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, dir, hard, target} {
		if SaveContact(path, c) == nil {
			t.Fatal("unsafe save accepted")
		}
		if DeleteContact(path) == nil {
			t.Fatal("unsafe deletion accepted")
		}
	}
	dirlink := filepath.Join(d, "linked-directory")
	if err := os.Symlink(dir, dirlink); err != nil {
		t.Fatal(err)
	}
	if SaveContact(filepath.Join(dirlink, "contact.json"), c) == nil {
		t.Fatal("symlink directory accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "contact.json")); !os.IsNotExist(err) {
		t.Fatal("wrote through symlink")
	}
	public := filepath.Join(d, "public")
	os.Mkdir(public, 0755)
	os.Chmod(public, 0755)
	if SaveContact(filepath.Join(public, "contact.json"), c) == nil {
		t.Fatal("public directory accepted")
	}
	p := filepath.Join(d, "public.json")
	os.WriteFile(p, []byte("old"), 0644)
	os.Chmod(p, 0644)
	if SaveContact(p, c) == nil || DeleteContact(p) == nil {
		t.Fatal("public file accepted")
	}
}

func TestContactStoreConcurrentAtomic(t *testing.T) {
	d := contactTestDirectory(t)
	p := filepath.Join(d, "contact.json")
	c := fixtureContact()
	if err := SaveContact(p, c); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v := c
			v.FirstName = strings.Repeat("A", i+1)
			for j := 0; j < 10; j++ {
				if err := SaveContact(p, v); err != nil {
					t.Error(err)
				}
				got, err := LoadContact(p)
				if err != nil || got.Email != c.Email || got.FirstName == "" {
					t.Error("non-atomic contact read")
				}
			}
		}(i)
	}
	wg.Wait()
	entries, _ := os.ReadDir(d)
	if len(entries) != 1 {
		t.Fatal("temporary files remained")
	}
}

func TestDeleteContactIsPrivateAndIdempotent(t *testing.T) {
	d := contactTestDirectory(t)
	p := filepath.Join(d, "contact.json")
	if err := DeleteContact(p); err != nil {
		t.Fatal(err)
	}
	if err := SaveContact(p, fixtureContact()); err != nil {
		t.Fatal(err)
	}
	if err := DeleteContact(p); err != nil {
		t.Fatal(err)
	}
	if err := DeleteContact(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("contact remained")
	}
}

func TestDefaultContactPathAndConfigFallback(t *testing.T) {
	d := contactTestDirectory(t)
	t.Setenv("HOME", d)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(d, "config"))
	p, err := DefaultContactPath()
	if err != nil || !filepath.IsAbs(p) || !strings.HasSuffix(p, filepath.Join("apple-store-helper", "private", "pickup-contact.json")) {
		t.Fatal("invalid default path")
	}
	if _, err := os.Stat(filepath.Dir(p)); !os.IsNotExist(err) {
		t.Fatal("default lookup created directory")
	}
	c, _ := testConfig(t)
	c.ContactFile = ""
	configPath := filepath.Join(d, "config.json")
	privateWrite(t, configPath, c)
	got, err := LoadConfig(configPath)
	if err != nil || got.ContactFile != p {
		t.Fatalf("default config not resolved: %v", err)
	}
	if c.Validate() == nil {
		t.Fatal("direct empty contact accepted")
	}
	c.ContactFile = "contacts/override.json"
	privateWrite(t, configPath, c)
	got, err = LoadConfig(configPath)
	if err != nil || got.ContactFile != filepath.Join(d, c.ContactFile) {
		t.Fatal("relative contact resolution changed")
	}
}

func TestContactStoreErrorsDoNotExposeContactOrPath(t *testing.T) {
	d := contactTestDirectory(t)
	c := fixtureContact()
	c.IDLast4 = "sensitive-invalid"
	secretPath := filepath.Join(d, "secret-contact-name.json")
	err := SaveContact(secretPath, c)
	if err == nil {
		t.Fatal("invalid accepted")
	}
	for _, secret := range []string{c.Email, c.Phone, c.IDLast4, secretPath} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("error exposed contact or path")
		}
	}
}

func TestDefaultContactPathDoesNotFallBackToWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path lookup")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if path, err := DefaultContactPath(); err == nil || path != "" {
		t.Fatal("missing user configuration location accepted")
	}
}
