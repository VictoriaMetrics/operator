package migrate

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// ForceMergeTimeout bounds a single force-merge admin call.
var ForceMergeTimeout = 10 * time.Minute

// ForceMerge issues a best-effort admin call to force compaction before a storage volume
// gets snapshotted. Never fatal: CSI snapshots are crash-consistent regardless.
func ForceMerge(ctx context.Context, httpClient *http.Client, url string) error {
	ctx, cancel := context.WithTimeout(ctx, ForceMergeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("cannot create force-merge request for %s: %w", url, err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("force-merge request to %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response status=%d from force-merge request to %s", resp.StatusCode, url)
	}
	return nil
}
