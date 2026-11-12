package reinvest

import (
	"strings"
	"testing"
)

// Each kind of key as it is pasted, with the part that must not survive.
var pastedKeys = []struct{ kind, text, secret string }{
	{"Anthropic", "use sk-ant-api03-" + strings.Repeat("Ab1", 10) + " here", "Ab1Ab1Ab1"},
	{"OpenAI project", "key sk-proj-" + strings.Repeat("Xy9_-", 10) + " ok", "Xy9_-Xy9"},
	{"OpenAI service account", "sk-svcacct-" + strings.Repeat("Q1w2", 10), "Q1w2Q1w2"},
	{"OpenAI classic", "sk-" + strings.Repeat("a1", 20), "a1a1a1"},
	{"GitHub", "ghp_" + strings.Repeat("Z9", 18), "Z9Z9Z9"},
	{"GitHub fine-grained", "github_pat_" + strings.Repeat("Ab_", 10), "Ab_Ab_"},
	{"GitLab", "glpat-" + strings.Repeat("Kp3", 8), "Kp3Kp3"},
	{"npm", "npm_" + strings.Repeat("N0p", 12), "N0pN0p"},
	{"Google", "AIza" + strings.Repeat("Sy7-", 9), "Sy7-Sy7"},
	{"Stripe", "sk_live_" + strings.Repeat("S7r", 9), "S7rS7r"},
	{"Hugging Face", "hf_" + strings.Repeat("Hf4", 12), "Hf4Hf4"},
	{"AWS", "AKIA" + strings.Repeat("A1B2", 4), "A1B2A1B2"},
	{"AWS temporary", "ASIA" + strings.Repeat("C3D4", 4), "C3D4C3D4"},
	{"AWS before an underscore", "AKIA" + strings.Repeat("E5F6", 4) + "_x", "E5F6E5F6"},
	{"Slack", "xoxb-" + strings.Repeat("12-", 6), "12-12-"},
	{"bearer token", "Authorization: Bearer " + strings.Repeat("tok.En", 5), "tok.Entok"},
	{"JWT", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "dozjgNryP4J3"},
	{"private key, body and all", "please debug\n-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\nQyNTUxOQAAACBXsecretpart\n-----END OPENSSH PRIVATE KEY-----\nthanks", "secretpart"},
	{"private key cut off", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA" + strings.Repeat("Zx", 20), "MIIEowIBAAK"},
}

func TestRedact_every_kind_of_pasted_key_is_removed(t *testing.T) {
	for _, k := range pastedKeys {
		s := addPrompt(session{Skills: map[string]bool{}}, "context words "+k.text+" end of it", nil)
		if len(s.Prompts) != 1 || strings.Contains(s.Prompts[0], k.secret) {
			t.Errorf("%s: kept %q", k.kind, s.Prompts)
		}
		if found := risks("---\nname: x\n---\n"+k.text+"\n", false); len(found) == 0 {
			t.Errorf("%s: a skill holding it passes the risk rules", k.kind)
		}
	}
	for _, plain := range []string{"the sk-learn library", "Bearer tokens expire", "run npm_config_prefix=x", "ask for the AIza prefix"} {
		if got := redact(plain); got != plain {
			t.Errorf("ordinary words lost: %q became %q", plain, got)
		}
	}
}
