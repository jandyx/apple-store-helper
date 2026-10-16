package power

import "testing"

func TestAcquireAndRelease(t *testing.T) {
	release, err := Acquire()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	release()
	release()
}
