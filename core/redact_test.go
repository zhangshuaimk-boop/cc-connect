package core

import (
	"strings"
	"testing"
)

func TestRedactArgs_FlagValue(t *testing.T) {
	args := []string{"--api-key", "sk-secret-123", "--verbose"}
	out := RedactArgs(args)

	if out[1] != "***" {
		t.Errorf("expected redacted, got %q", out[1])
	}
	if out[2] != "--verbose" {
		t.Errorf("non-sensitive arg modified: %q", out[2])
	}
}

func TestRedactArgs_EqualFormat(t *testing.T) {
	args := []string{"--api-key=sk-secret-123", "--verbose"}
	out := RedactArgs(args)

	if !strings.HasPrefix(out[0], "--api-key=") || !strings.HasSuffix(out[0], "***") {
		t.Errorf("expected --api-key=***, got %q", out[0])
	}
}

func TestRedactArgs_MultipleFlags(t *testing.T) {
	args := []string{"--token", "tok-123", "--secret", "s3cr3t", "--model", "gpt-4"}
	out := RedactArgs(args)

	if out[1] != "***" {
		t.Errorf("--token value not redacted: %q", out[1])
	}
	if out[3] != "***" {
		t.Errorf("--secret value not redacted: %q", out[3])
	}
	if out[5] != "gpt-4" {
		t.Errorf("--model value should not be redacted: %q", out[5])
	}
}

func TestRedactArgs_NoModifyOriginal(t *testing.T) {
	args := []string{"--api-key", "sk-secret"}
	_ = RedactArgs(args)
	if args[1] != "sk-secret" {
		t.Error("original args should not be modified")
	}
}

func TestRedactArgs_ShortFlag(t *testing.T) {
	args := []string{"-k", "my-key"}
	out := RedactArgs(args)
	if out[1] != "***" {
		t.Errorf("-k value not redacted: %q", out[1])
	}
}

func TestRedactArgs_Empty(t *testing.T) {
	out := RedactArgs(nil)
	if len(out) != 0 {
		t.Errorf("expected empty, got %v", out)
	}
}

func TestRedactEnv_MasksSensitiveKeys(t *testing.T) {
	env := []string{
		"FEISHU_APP_ID=cli_xxx",
		"FEISHU_APP_SECRET=real-secret",
		"LARK_TOKEN=tenant-token",
		"CREDENTIAL_PATH=/tmp/credential.json",
		"NORMAL=value",
		"NO_EQUALS",
	}
	out := RedactEnv(env)

	want := []string{
		"FEISHU_APP_ID=cli_xxx",
		"FEISHU_APP_SECRET=***",
		"LARK_TOKEN=***",
		"CREDENTIAL_PATH=***",
		"NORMAL=value",
		"NO_EQUALS",
	}
	if len(out) != len(want) {
		t.Fatalf("len(out) = %d, want %d", len(out), len(want))
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("out[%d] = %q, want %q", i, out[i], want[i])
		}
	}
	if env[1] != "FEISHU_APP_SECRET=real-secret" {
		t.Errorf("original env modified: %v", env)
	}
}

func TestRedactEnv_CaseInsensitiveAndKeepsEmptyValues(t *testing.T) {
	env := []string{
		"custom_key=abc",
		"password=",
		"api_secret=s3",
		"PUBLIC_SECRET_HINTED=visible",
		"SAFE=value",
	}
	out := RedactEnv(env)

	for _, idx := range []int{0, 1, 2, 3} {
		if !strings.HasSuffix(out[idx], "=***") {
			t.Errorf("out[%d] = %q, want redacted value", idx, out[idx])
		}
	}
	if out[4] != "SAFE=value" {
		t.Errorf("non-sensitive env modified: %q", out[4])
	}
}

func TestRedactArgs_AppSecretAndCaseInsensitiveFlags(t *testing.T) {
	args := []string{
		"--api-key=api-key",
		"--SECRET", "app-secret",
		"--Password=pass",
		"--token", "tenant-token",
		"--app-id", "cli_xxx",
	}
	out := RedactArgs(args)

	if out[0] != "--api-key=***" {
		t.Errorf("app key equal flag not redacted: %q", out[0])
	}
	if out[2] != "***" {
		t.Errorf("--SECRET value not redacted: %q", out[2])
	}
	if out[3] != "--Password=***" {
		t.Errorf("--Password equal flag not redacted: %q", out[3])
	}
	if out[5] != "***" {
		t.Errorf("--token value not redacted: %q", out[5])
	}
	if out[7] != "cli_xxx" {
		t.Errorf("--app-id should not be treated as secret: %q", out[7])
	}
}

func TestRedactArgs_SensitiveFlagWithoutValue(t *testing.T) {
	args := []string{"--token"}
	out := RedactArgs(args)
	if len(out) != 1 || out[0] != "--token" {
		t.Errorf("flag without value should be preserved, got %v", out)
	}
}
