package hashutil_test

import (
	"strings"
	"testing"

	"github.com/jskswamy/aide/internal/hashutil"
)

func TestHexSumIsSHA256Hex(t *testing.T) {
	got := hashutil.New("v1").Field("hello").HexSum()
	if len(got) != 64 {
		t.Errorf("hex sum length = %d, want 64", len(got))
	}
	if strings.ContainsAny(got, "GHIJKLMNOPQRSTUVWXYZ") {
		t.Errorf("hex sum %q contains non-hex chars", got)
	}
}

func TestVersionTagDistinguishesHashes(t *testing.T) {
	a := hashutil.New("a-v1").Field("same").HexSum()
	b := hashutil.New("b-v1").Field("same").HexSum()
	if a == b {
		t.Errorf("version tag should distinguish hashes; got %q for both", a)
	}
}

func TestFieldsAreInjective(t *testing.T) {
	// Length-prefixing must defeat the classic SHA-concat collision:
	// (Field("ab"), Field("c")) and (Field("a"), Field("bc")) carry the
	// same raw bytes but must not collide.
	left := hashutil.New("v1").Field("ab").Field("c").HexSum()
	right := hashutil.New("v1").Field("a").Field("bc").HexSum()
	if left == right {
		t.Errorf("length-prefix encoding leaked: %q == %q", left, right)
	}
}

func TestNewlineInFieldDoesNotCollide(t *testing.T) {
	// Trust's old format used path + "\n" + contents. With length-prefixed
	// fields, a path that contains a newline cannot impersonate a
	// (path, contents) pair.
	a := hashutil.New("trust-v1").Field("a\nb").Bytes(nil).HexSum()
	b := hashutil.New("trust-v1").Field("a").Bytes([]byte("b")).HexSum()
	if a == b {
		t.Errorf("newline-in-field collided with field boundary: %q == %q", a, b)
	}
}

func TestBytesEncodesLength(t *testing.T) {
	a := hashutil.New("v1").Bytes([]byte("hello")).HexSum()
	b := hashutil.New("v1").Field("hello").HexSum()
	// Bytes and Field share encoding so callers can mix them freely.
	if a != b {
		t.Errorf("Bytes and Field of identical content should match: %q vs %q", a, b)
	}
}

func TestEmptyFieldIsDistinctFromNoField(t *testing.T) {
	a := hashutil.New("v1").HexSum()
	b := hashutil.New("v1").Field("").HexSum()
	if a == b {
		t.Errorf("empty Field should still alter the hash; got identical %q", a)
	}
}
