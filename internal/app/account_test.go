package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Azd325/router-axi/internal/tr064"
)

func (f fakeReader) Account(context.Context) (tr064.Account, error) {
	defaultPassword := false
	return tr064.Account{Username: "synthetic-account", Rights: []tr064.AccountRight{{Path: "BoxAdmin", Access: "none"}, {Path: "NAS", Access: "readonly"}}, DefaultPasswordActive: &defaultPassword, SecondFactorEnabled: true}, f.err
}

func TestAccountOutput(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"account"}, "account:\n  username: \"synthetic-account\"\n  anonymous_login_enabled: false\n  default_password_active: false\n  second_factor_enabled: true\n  rights[2]{path,access}:\n    BoxAdmin,none\n    NAS,readonly\n"},
		{[]string{"account", "--json"}, `{"username":"synthetic-account","rights":[{"path":"BoxAdmin","access":"none"},{"path":"NAS","access":"readonly"}],"anonymous_login_enabled":false,"default_password_active":false,"second_factor_enabled":true}` + "\n"},
	} {
		code, stdout, stderr := runTest(t, test.args...)
		if code != ExitOK || stdout != test.want || stderr != "" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	}
}

type accountStateReader struct {
	fakeReader
	account tr064.Account
}

func (r accountStateReader) Account(context.Context) (tr064.Account, error) { return r.account, r.err }

func TestAccountUnknownAndEmptyRights(t *testing.T) {
	for _, test := range []struct {
		rights           []tr064.AccountRight
		want, jsonRights string
	}{
		{nil, "  rights: unknown", "null"},
		{[]tr064.AccountRight{}, "  rights: 0 configured rights reported", "[]"},
	} {
		application := New(func(Config) (Reader, error) {
			return accountStateReader{account: tr064.Account{Rights: test.rights}}, nil
		}, func(string) string { return "" })
		for _, jsonOutput := range []bool{false, true} {
			var stdout, stderr bytes.Buffer
			args := []string{"account"}
			if jsonOutput {
				args = append(args, "--json")
			}
			code := application.Run(t.Context(), args, &stdout, &stderr)
			want := test.want
			if jsonOutput {
				want = `{"username":"","rights":` + test.jsonRights + `,"anonymous_login_enabled":false,"default_password_active":null,"second_factor_enabled":false}` + "\n"
			}
			if code != ExitOK || !strings.Contains(stdout.String(), want) || stderr.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !jsonOutput && (!strings.Contains(stdout.String(), "default_password_active: unknown") || !strings.Contains(stdout.String(), `username: ""`)) {
				t.Fatalf("output=%s", &stdout)
			}
		}
	}
}

func TestAccountHelpAndInvalidArgumentsOffline(t *testing.T) {
	application := New(func(Config) (Reader, error) { t.Fatal("offline request constructed reader"); return nil, nil }, func(string) string { t.Fatal("offline request read environment"); return "" })
	for _, args := range [][]string{{"account", "--help"}, {"account", "--confirm"}, {"account", "--all"}, {"account", "--instance", "1"}, {"account", "--username", "private"}, {"account", "other"}} {
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), args, &stdout, &stderr)
		if args[1] == "--help" {
			if code != ExitOK || !strings.Contains(stdout.String(), "usage: router-axi account") {
				t.Fatalf("code=%d output=%s", code, &stdout)
			}
		} else if code != ExitUsage || !strings.Contains(stdout.String(), "invalid_arguments") {
			t.Fatalf("code=%d output=%s", code, &stdout)
		}
		if stderr.Len() != 0 || strings.Contains(stdout.String(), "private") {
			t.Fatalf("stdout=%s stderr=%s", &stdout, &stderr)
		}
	}
}

func TestAccountStructuredErrors(t *testing.T) {
	for _, test := range []struct {
		kind, code string
		exit       int
	}{{"auth", "authentication_failed", ExitAuth}, {"network", "router_unreachable", ExitNetwork}, {"unsupported", "unsupported_capability", ExitUnsupported}, {"protocol", "router_protocol_error", ExitRouter}, {"router", "router_protocol_error", ExitRouter}} {
		application := New(func(Config) (Reader, error) {
			return fakeReader{err: &tr064.Error{Kind: test.kind, Operation: "account", Message: "account inspection failed"}}, nil
		}, func(string) string { return "" })
		var stdout, stderr bytes.Buffer
		code := application.Run(t.Context(), []string{"account", "--json"}, &stdout, &stderr)
		if code != test.exit || !strings.Contains(stdout.String(), `"code":"`+test.code+`"`) || stderr.Len() != 0 {
			t.Fatalf("code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
		}
	}
}
