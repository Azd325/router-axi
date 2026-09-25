package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

const backupTestPassphrase = "private-export-passphrase"

func backupPayloadSum() string {
	sum := sha256.Sum256([]byte(syntheticBackupPayload))
	return hex.EncodeToString(sum[:])
}

func runBackupTest(t *testing.T, getenv func(string) string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, backupTestGetenv(getenv))
	code := application.Run(t.Context(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// backupTestGetenv always supplies the export passphrase, so callers cannot
// accidentally depend on the router login password being reused.
func backupTestGetenv(getenv func(string) string) func(string) string {
	return func(key string) string {
		if key == "ROUTER_AXI_BACKUP_PASSWORD" {
			return backupTestPassphrase
		}
		if getenv == nil {
			return ""
		}
		return getenv(key)
	}
}

func TestBackupOutputContract(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "fritz.export")
		args := []string{"backup", "--output", path}
		if jsonOutput {
			args = append(args, "--json")
		}
		code, stdout, stderr := runBackupTest(t, func(string) string { return "" }, args...)
		if code != ExitOK || stderr != "" {
			t.Fatalf("json=%t code=%d stdout=%q stderr=%q", jsonOutput, code, stdout, stderr)
		}
		if jsonOutput {
			want := `{"backup":{"path":"` + filepath.ToSlash(path) + `","bytes":` + strconv.Itoa(len(syntheticBackupPayload)) + `,"sha256":"` + backupPayloadSum() + `"}}` + "\n"
			if stdout != want {
				t.Fatalf("json=%t stdout=%q want=%q", jsonOutput, stdout, want)
			}
			continue
		}
		want := "backup:\n  path: " + strconv.Quote(path) + "\n  bytes: " + strconv.Itoa(len(syntheticBackupPayload)) + "\n  sha256: " + backupPayloadSum() + "\nnext: keep the export passphrase safe; the backup can only be restored with it\n"
		if stdout != want {
			t.Fatalf("stdout=%q want=%q", stdout, want)
		}
		written, err := os.ReadFile(path)
		if err != nil || string(written) != syntheticBackupPayload {
			t.Fatalf("content=%q err=%v", written, err)
		}
	}
}

func TestBackupGrammarBeforeFactory(t *testing.T) {
	for _, args := range [][]string{
		{"backup"}, {"backup", "--output"}, {"backup", "--output", ""},
		{"backup", "--output", "-starts-with-dash"}, {"backup", "--force"}, {"backup", "--all"},
		{"backup", "--confirm"}, {"backup", "--instance", "1"}, {"backup", "extra"},
		{"backup", "--password=synthetic-sensitive"}, {"backup", "--unknown"},
		{"status", "--output", "fritz.export"}, {"status", "--force"},
		{"--force"}, {"--output", "fritz.export"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			application := New(func(Config) (Reader, error) {
				t.Fatal("invalid grammar reached the client factory")
				return nil, nil
			}, func(string) string { return "" })
			input := append([]string(nil), args...)
			if jsonOutput {
				input = append(input, "--json")
			}
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), input, &stdout, &stderr)
			if code != ExitUsage || stdout.Len() != 0 || stderr.Len() == 0 || (jsonOutput && !json.Valid(stderr.Bytes())) {
				t.Fatalf("invalid grammar not rejected: %v", args)
			}
		}
	}
}

func TestBackupHelpIsOffline(t *testing.T) {
	application := New(func(Config) (Reader, error) {
		t.Fatal("help reached the client factory")
		return nil, nil
	}, func(string) string { return "" })
	for _, args := range [][]string{{"backup", "--output", "fritz.export", "--help"}, {"backup", "--help"}, {"backup", "-h"}} {
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		if code != ExitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "--force") || !strings.Contains(stdout.String(), "ROUTER_AXI_BACKUP_PASSWORD") || !strings.Contains(stdout.String(), "never overwritten without --force") {
			t.Fatalf("backup help omitted its safety contract: %v", args)
		}
	}
}

func TestBackupPassphraseIsEnvironmentOnly(t *testing.T) {
	exported := false
	application := New(func(Config) (Reader, error) {
		return passphraseRecorder{exported: &exported}, nil
	}, func(key string) string {
		// The router login password must never be reused as the export
		// passphrase.
		return map[string]string{"ROUTER_AXI_PASSWORD": "private-login-password"}[key]
	})
	var stdout, stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "fritz.export")
	code := application.Run(t.Context(), []string{"backup", "--output", path}, &stdout, &stderr)
	if code != ExitUsage || stdout.Len() != 0 || exported || !strings.Contains(stderr.String(), "backup_passphrase_missing") || !strings.Contains(stderr.String(), "ROUTER_AXI_BACKUP_PASSWORD") {
		t.Fatalf("code=%d exported=%t stderr=%q", code, exported, stderr.String())
	}
	if strings.Contains(stderr.String(), "private-login-password") {
		t.Fatal("the login password leaked into the backup error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a missing passphrase still created a file")
	}
}

type passphraseRecorder struct {
	fakeReader
	exported *bool
}

func (r passphraseRecorder) ConfigExport(_ context.Context, passphrase string) ([]byte, error) {
	if passphrase != "" {
		*r.exported = true
	}
	return nil, &tr064.Error{Kind: "protocol", Operation: "backup", Message: "synthetic export failure"}
}

func TestBackupNeverOverwritesWithoutForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fritz.export")
	if err := os.WriteFile(path, []byte("synthetic-previous-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runBackupTest(t, func(string) string { return "" }, "backup", "--output", path)
	if code != ExitUsage || stdout != "" || !strings.Contains(stderr, "output_exists") || !strings.Contains(stderr, "--force") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "synthetic-previous-content" {
		t.Fatalf("content=%q err=%v", content, err)
	}

	code, stdout, stderr = runBackupTest(t, func(string) string { return "" }, "backup", "--output", path, "--force")
	if code != ExitOK || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	content, err = os.ReadFile(path)
	if err != nil || string(content) != syntheticBackupPayload {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestBackupWritesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fritz.export")

	// A failing export must leave neither the target nor a temporary file.
	application := New(func(Config) (Reader, error) {
		return fakeReader{err: &tr064.Error{Kind: "network", Operation: "backup", Message: "router could not be reached during the configuration export"}}, nil
	}, backupTestGetenv(nil))
	var stdout, stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"backup", "--output", path}, &stdout, &stderr); code != ExitNetwork || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("a failed export left %d files: %v", len(entries), err)
	}

	// A successful export writes exactly the destination, owner-only.
	code, _, stderrText := runBackupTest(t, func(string) string { return "" }, "backup", "--output", path)
	if code != ExitOK || stderrText != "" {
		t.Fatalf("code=%d stderr=%q", code, stderrText)
	}
	entries, err = os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "fritz.export" {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode().Perm(), err)
	}
}

func TestBackupOverwriteRaceIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fritz.export")
	// The export finishes while the destination appears, so only the atomic
	// no-replace write can refuse the overwrite.
	application := New(func(Config) (Reader, error) {
		return racingReader{path: path}, nil
	}, backupTestGetenv(nil))
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"backup", "--output", path}, &stdout, &stderr)
	if code != ExitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), "output_exists") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "synthetic-raced-content" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

type racingReader struct {
	fakeReader
	path string
}

func (r racingReader) ConfigExport(_ context.Context, _ string) ([]byte, error) {
	if err := os.WriteFile(r.path, []byte("synthetic-raced-content"), 0o600); err != nil {
		return nil, err
	}
	return []byte(syntheticBackupPayload), nil
}

func TestBackupRejectsUnsafeOutputPaths(t *testing.T) {
	dir := t.TempDir()
	for _, test := range []struct {
		name, path, want string
	}{
		{"directory", dir, "invalid_output"},
		{"directory with separator", dir + string(os.PathSeparator), "invalid_output"},
		{"missing parent", filepath.Join(dir, "missing", "fritz.export"), "invalid_output"},
		{"empty", "", "invalid_arguments"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"backup"}
			if test.path != "" {
				args = append(args, "--output", test.path)
			}
			application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, backupTestGetenv(nil))
			var stdout, stderr bytes.Buffer
			code := application.Run(t.Context(), args, &stdout, &stderr)
			if code != ExitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestBackupWriteFailureIsSanitized(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a read-only directory does not block the root user")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fritz.export")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	code, stdout, stderr := runBackupTest(t, func(string) string { return "" }, "backup", "--output", path)
	if code != ExitInternal || stdout != "" || !strings.Contains(stderr, "backup_write_failed") || strings.Contains(stderr, syntheticBackupPayload) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestBackupOutputWriteFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fritz.export")
	application := New(func(Config) (Reader, error) { return fakeReader{}, nil }, backupTestGetenv(nil))
	var stderr bytes.Buffer
	if code := application.Run(t.Context(), []string{"backup", "--output", path}, failingWriter{}, &stderr); code != ExitInternal || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

// backupFixtureServer serves the documented export flow over TLS for
// end-to-end command tests: discovery, one SOAP action that must carry the
// passphrase from ROUTER_AXI_BACKUP_PASSWORD, and the one-time HTTPS download.
func backupFixtureServer(t *testing.T, getConfigFileResponse string) *httptest.Server {
	t.Helper()
	const description = `<root><device><serviceList><service><serviceType>urn:dslforum-org:service:DeviceConfig:1</serviceType><controlURL>/config</controlURL></service></serviceList></device></root>`
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tr64desc.xml":
			_, _ = io.WriteString(w, description)
		case r.Method == http.MethodPost && r.URL.Path == "/config" && r.Header.Get("SOAPAction") == `"urn:dslforum-org:service:DeviceConfig:1#X_AVM-DE_GetConfigFile"`:
			body, err := io.ReadAll(r.Body)
			if err != nil || !strings.Contains(string(body), "<NewX_AVM-DE_Password>"+backupTestPassphrase+"</NewX_AVM-DE_Password>") {
				t.Error("the export request did not carry the environment passphrase")
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = io.WriteString(w, strings.ReplaceAll(getConfigFileResponse, "__ORIGIN__", r.Host))
		case r.Method == http.MethodGet && r.URL.Path == "/TR064/synthetic-export-token":
			_, _ = io.WriteString(w, syntheticBackupPayload)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestBackupClientBoundary(t *testing.T) {
	server := backupFixtureServer(t, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:X_AVM-DE_GetConfigFileResponse xmlns:u="urn:dslforum-org:service:DeviceConfig:1"><NewX_AVM-DE_ConfigFileUrl>https://__ORIGIN__/TR064/synthetic-export-token</NewX_AVM-DE_ConfigFileUrl></u:X_AVM-DE_GetConfigFileResponse></s:Body></s:Envelope>`)
	path := filepath.Join(t.TempDir(), "fritz.export")
	application := New(func(config Config) (Reader, error) {
		return tr064.New(config.Host, config.Username, config.Password, server.Client())
	}, func(key string) string {
		if key == "ROUTER_AXI_BACKUP_PASSWORD" {
			return backupTestPassphrase
		}
		return ""
	})
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"backup", "--output", path, "--host", server.URL}, &stdout, &stderr)
	if code != ExitOK || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != syntheticBackupPayload {
		t.Fatalf("content=%q err=%v", content, err)
	}
	output := stdout.String() + stderr.String()
	for _, secret := range []string{backupTestPassphrase, "synthetic-export-token", syntheticBackupPayload, server.URL} {
		if strings.Contains(output, secret) {
			t.Fatal("the backup output leaked the passphrase, the download token, or the export content")
		}
	}
	if !strings.Contains(stdout.String(), backupPayloadSum()) {
		t.Fatal("the backup output omitted the export checksum")
	}
}

func TestBackupClientFailuresAreSanitized(t *testing.T) {
	server := backupFixtureServer(t, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><errorCode>private-code</errorCode><errorDescription>synthetic-sensitive-fault</errorDescription></s:Fault></s:Body></s:Envelope>`)
	path := filepath.Join(t.TempDir(), "fritz.export")
	application := New(func(config Config) (Reader, error) {
		return tr064.New(config.Host, config.Username, config.Password, server.Client())
	}, func(key string) string {
		if key == "ROUTER_AXI_BACKUP_PASSWORD" {
			return backupTestPassphrase
		}
		return ""
	})
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"backup", "--output", path, "--host", server.URL}, &stdout, &stderr)
	if code != ExitRouter || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "synthetic-sensitive") || strings.Contains(stdout.String()+stderr.String(), backupTestPassphrase) || strings.Contains(stdout.String()+stderr.String(), server.URL) {
		t.Fatal("the backup failure leaked router data")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a failed backup wrote a file")
	}
}

func TestBackupRefusesPlaintextOriginBeforeContactingRouter(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "fritz.export")
	application := New(func(config Config) (Reader, error) {
		return tr064.New(config.Host, config.Username, config.Password, server.Client())
	}, backupTestGetenv(nil))
	for _, jsonOutput := range []bool{false, true} {
		args := []string{"backup", "--output", path, "--host", server.URL}
		if jsonOutput {
			args = append(args, "--json")
		}
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		if code != ExitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), "backup_requires_https") || !strings.Contains(stderr.String(), "--host https://") || strings.Contains(stderr.String(), backupTestPassphrase) {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	}
	if requests.Load() != 0 {
		t.Fatal("a plaintext origin received a request")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a refused backup wrote a file")
	}
}

func TestBackupReportsUntrustedCertificate(t *testing.T) {
	server := backupFixtureServer(t, "")
	path := filepath.Join(t.TempDir(), "fritz.export")
	application := New(func(config Config) (Reader, error) {
		return tr064.New(config.Host, config.Username, config.Password, nil)
	}, backupTestGetenv(nil))
	var stdout, stderr bytes.Buffer
	code := application.Run(t.Context(), []string{"backup", "--output", path, "--host", server.URL, "--json"}, &stdout, &stderr)
	var payload struct {
		Error struct{ Code, Message, Hint string } `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if code != ExitNetwork || stdout.Len() != 0 || payload.Error.Code != "tls_untrusted" || !strings.Contains(payload.Error.Hint, "certificate") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("an untrusted certificate wrote a file")
	}
}

func TestBackupReportsUnsupportedHardLinks(t *testing.T) {
	original := linkFile
	t.Cleanup(func() { linkFile = original })
	for _, errno := range []syscall.Errno{syscall.ENOTSUP, syscall.EPERM, syscall.EXDEV} {
		t.Run(errno.Error(), func(t *testing.T) {
			linkFile = func(oldname, newname string) error {
				return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: errno}
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "fritz.export")
			code, stdout, stderr := runBackupTest(t, nil, "backup", "--output", path)
			if code != ExitInternal || stdout != "" || !strings.Contains(stderr, "backup_link_unsupported") || !strings.Contains(stderr, "--force") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("entries=%v err=%v", entries, err)
			}
		})
	}
}
