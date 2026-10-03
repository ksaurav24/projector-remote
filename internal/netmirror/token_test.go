package netmirror

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

const (
	tokOld = "0123456789abcdef0123456789abcdef::fedcba9876543210fedcba9876543210::1790000000::ni"
	tokNew = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa::bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb::1790086400::ni"
)

// mmkvFile lays out body the way MMKV does: a little-endian uint32 of
// the valid length, the data, then the rest of the page, which may
// hold leftovers from before the last compaction.
func mmkvFile(body, tail string) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(body)))
	return append(append(b, body...), tail...)
}

func TestLastToken_PicksLatestWrite(t *testing.T) {
	// MMKV appends each write, so an updated key leaves its old value
	// earlier in the file. Binary length prefixes sit between entries.
	blob := mmkvFile("\x00\x10\x09usertoken\x52"+tokOld+"\x03ott\x02pv\x09usertoken\x52"+tokNew+"\x00", "")
	if got := lastToken(blob); got != tokNew {
		t.Fatalf("lastToken = %q, want %q", got, tokNew)
	}
}

func TestLastToken_IgnoresStaleTail(t *testing.T) {
	// After a compaction rewrites the file shorter, an older token can
	// survive past the valid length. It must not win.
	blob := mmkvFile("\x09usertoken\x52"+tokNew, "\x00\x09usertoken\x52"+tokOld)
	if got := lastToken(blob); got != tokNew {
		t.Fatalf("lastToken = %q, want %q", got, tokNew)
	}
}

func TestLastToken_NoneIsEmpty(t *testing.T) {
	if got := lastToken(mmkvFile("\x00\x03ott\x02nf", "")); got != "" {
		t.Fatalf("lastToken = %q, want empty", got)
	}
}

func TestMMKVToken_ReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mmkv.default")
	if err := os.WriteFile(path, mmkvFile("\x09usertoken\x52"+tokNew, ""), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := MMKVToken{Path: path}.Token(context.Background())
	if err != nil || got != tokNew {
		t.Fatalf("Token() = %q, %v", got, err)
	}
}

type fallbackToken struct {
	tok string
	err error
}

func (f fallbackToken) Token(context.Context) (string, error) { return f.tok, f.err }

func TestTokenFile_SavedTokenIsUsed(t *testing.T) {
	f := TokenFile{Path: filepath.Join(t.TempDir(), "token"), Fallback: fallbackToken{err: os.ErrPermission}}
	if err := f.Save(tokNew); err != nil {
		t.Fatal(err)
	}
	if got, err := f.Token(context.Background()); err != nil || got != tokNew {
		t.Fatalf("Token() = %q, %v", got, err)
	}
}

func TestTokenFile_NewerTokenWins(t *testing.T) {
	dir := t.TempDir()
	// Logged in on the remote, then later in the NetMirror app.
	f := TokenFile{Path: filepath.Join(dir, "token"), Fallback: fallbackToken{tok: tokNew}}
	if err := f.Save(tokOld); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Token(context.Background()); got != tokNew {
		t.Fatalf("app newer: Token() = %q, want %q", got, tokNew)
	}
	// And the other way round.
	f = TokenFile{Path: filepath.Join(dir, "token"), Fallback: fallbackToken{tok: tokOld}}
	if err := f.Save(tokNew); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Token(context.Background()); got != tokNew {
		t.Fatalf("remote newer: Token() = %q, want %q", got, tokNew)
	}
}

func TestTokenFile_NothingSavedUsesFallback(t *testing.T) {
	f := TokenFile{Path: filepath.Join(t.TempDir(), "token"), Fallback: fallbackToken{tok: tokOld}}
	if got, err := f.Token(context.Background()); err != nil || got != tokOld {
		t.Fatalf("Token() = %q, %v", got, err)
	}
}
