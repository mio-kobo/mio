package bunkopm

import (
	"fmt"
	"net/url"
	"strings"
)

func (f *Fetcher) VerifyRelease(r Record) (int64, error) {
	repo := repository.FindStringSubmatch(r.Repository)
	if repo == nil {
		return 0, fail("BUNKO_E_SOURCE_VERIFY", "invalid repository")
	}
	base := api + "/repos/" + repo[1] + "/" + repo[2]
	var release struct {
		ID        jsonID `json:"id"`
		Draft     *bool  `json:"draft"`
		Immutable bool   `json:"immutable"`
		Tag       string `json:"tag_name"`
		Assets    []struct {
			ID               jsonID `json:"id"`
			URL, Name, State string
			Size             int64
			Digest           *string
		}
	}
	if e := f.json(base+"/releases/"+r.ReleaseID, &release); e != nil {
		return 0, e
	}
	if string(release.ID) != r.ReleaseID || release.Draft == nil || *release.Draft || !release.Immutable || release.Tag == "" {
		return 0, fail("BUNKO_E_SOURCE_VERIFY", "release must be published and immutable")
	}
	assetID := r.Archive[strings.LastIndex(r.Archive, "/")+1:]
	var size int64
	count := 0
	for _, a := range release.Assets {
		if string(a.ID) == assetID {
			count++
			if !strings.EqualFold(a.URL, r.Archive) || !strings.HasSuffix(a.Name, ".tar.gz") || a.State != "uploaded" || a.Size <= 0 || a.Size > MaxArchive || (a.Digest != nil && *a.Digest != "sha256:"+r.SHA256) {
				return 0, fail("BUNKO_E_SOURCE_VERIFY", "asset identity, size or digest mismatch")
			}
			size = a.Size
		}
	}
	if count != 1 {
		return 0, fail("BUNKO_E_SOURCE_VERIFY", "archive asset is not unique in release")
	}
	var ref struct{ Object struct{ SHA, Type string } }
	if e := f.json(base+"/git/ref/tags/"+url.PathEscape(release.Tag), &ref); e != nil {
		return 0, e
	}
	for j := 0; j < 3 && ref.Object.Type == "tag"; j++ {
		if !hex40.MatchString(ref.Object.SHA) {
			return 0, fail("BUNKO_E_SOURCE_VERIFY", "invalid tag SHA")
		}
		if e := f.json(base+"/git/tags/"+ref.Object.SHA, &ref); e != nil {
			return 0, e
		}
	}
	if ref.Object.Type != "commit" || ref.Object.SHA != r.SourceCommit {
		return 0, fail("BUNKO_E_SOURCE_VERIFY", "release tag differs from source_commit")
	}
	return size, nil
}

// IDs are decimal strings in TOML and JSON numbers in the GitHub API. Retaining
// their bytes avoids the loss of integer precision from float64 decoding.
type jsonID string

func (n *jsonID) UnmarshalJSON(b []byte) error {
	if !id.Match(b) {
		return fmt.Errorf("invalid GitHub ID")
	}
	*n = jsonID(b)
	return nil
}
