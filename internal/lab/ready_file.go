package lab

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// writeReady mints an owner token for this run and writes the ready file:
// a script's way into a held lab (#1197). Written to a temporary name and
// renamed, so a script that sees the file sees all of it.
func (r *Runner) writeReady(ctx context.Context, lab *Lab, path string) error {
	token, err := mintToken(ctx, lab.client, lab.BaseURL)
	if err != nil {
		return err
	}
	body := FormatReadyEnv(ReadyVars(lab, token))
	tmp, err := os.CreateTemp(filepath.Dir(path), ".lab-ready-*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// mintToken mints an API token for the lab's login through the API, with
// the session the readiness reads signed in with.
func mintToken(ctx context.Context, c *http.Client, base string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/auth/me/tokens",
		strings.NewReader(`{"description":"a script's lab (omniglass lab run --ready-file)","ttl_days":1}`))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Token string `json:"token"`
	}
	if err := doJSON(c, req, http.StatusCreated, &out); err != nil {
		return "", fmt.Errorf("mint the ready file's token: %w", err)
	}
	if out.Token == "" {
		return "", fmt.Errorf("mint the ready file's token: the answer carried none")
	}
	return out.Token, nil
}

// ReadyVars are the ready file's variables, in order: how a container on
// the lab's network reaches it (the network, the server's address there, an
// owner token and the login), its containers, the labels a helper container
// carries so the lab's teardown and the reaper treat it as the lab's own,
// and the lab's process.
func ReadyVars(lab *Lab, token string) [][2]string {
	s := lab.Spec
	labels := OwnedLabels(s)
	pairs := make([]string, 0, len(labels))
	for _, k := range SortedKeys(labels) {
		pairs = append(pairs, k+"="+labels[k])
	}
	return [][2]string{
		{"LAB_PROJECT", s.Project},
		{"LAB_NETWORK", s.Network},
		{"LAB_SERVER_URL", ServerURL},
		{"LAB_HOST_URL", lab.BaseURL},
		{"LAB_TOKEN", token},
		{"LAB_USER", LoginUser},
		{"LAB_PASSWORD", LoginPassword},
		{"LAB_IMAGE", s.Server.Image},
		{"LAB_DSN", DSN},
		{"LAB_DB", s.DB.Container},
		{"LAB_SERVER", s.Server.Container},
		{"LAB_NODE", s.Node.Container},
		{"LAB_LABELS", strings.Join(pairs, " ")},
		{"LAB_OWNER_PID", strconv.Itoa(s.Owner.PID)},
	}
}

// FormatReadyEnv renders variables as shell assignments a script sources,
// each value single-quoted.
func FormatReadyEnv(vars [][2]string) string {
	var b strings.Builder
	for _, v := range vars {
		fmt.Fprintf(&b, "%s='%s'\n", v[0], strings.ReplaceAll(v[1], "'", `'\''`))
	}
	return b.String()
}
