package vault

import "testing"

func TestSealOpen(t *testing.T) {
	in := map[string]string{"session:Lab/sw1": "s3cret", "credential:lab": "pw"}
	data, err := Seal("correct horse", in)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if err := Open("wrong", data, &out); err != ErrBadPassphrase {
		t.Fatalf("expected bad passphrase, got %v", err)
	}
	if err := Open("correct horse", data, &out); err != nil || out["session:Lab/sw1"] != "s3cret" || len(out) != 2 {
		t.Fatalf("%v %v", err, out)
	}
}

func TestMasterPassword(t *testing.T) {
	h, err := HashPassword("m@ster")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword("m@ster", h) || CheckPassword("master", h) || CheckPassword("m@ster", "junk") {
		t.Fatal("verifier")
	}
}
