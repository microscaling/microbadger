package inspector

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/microscaling/microbadger/database"
)

func TestGetHashFromLayers(t *testing.T) {
	layers := []database.ImageLayer{
		{BlobSum: "10000", Command: "abcdefabcdefabcdefabcdef", DownloadSize: 345},
		{BlobSum: "20000", Command: "aaaaaaaaaaaaaaaaaaaaaaaa", DownloadSize: 345},
		{BlobSum: "30000", Command: "xx", DownloadSize: 345},
	}

	checksum := sha256.Sum256([]byte("abcdefabcdefabcdefabcdef" + "aaaaaaaaaaaaaaaaaaaaaaaa" + "xx"))
	expectedResult := hex.EncodeToString(checksum[:])

	result := GetHashFromLayers(layers)
	if result != expectedResult {
		t.Errorf("Unexpected hash result")
	}
}

func TestParseGithubLabels(t *testing.T) {
	var vcs *database.VersionControl
	var tests []string

	httpsString := "https://github.com/microscaling/microscaling.git"
	sshString := "git@github.com:microscaling/microscaling.git"
	result := "https://github.com/microscaling/microscaling/tree/12345"

	tests = []string{
		httpsString, sshString,
	}

	for _, test := range tests {
		vcs = &database.VersionControl{}
		vcs.URL = test
		vcs.Commit = "12345"
		vcs = parseGitHubLabels(vcs)
		if vcs.Type != "git" {
			t.Fatalf("Wrong VCS type: %s", vcs.Type)
		}

		if vcs.URL != result {
			t.Fatalf("Wrong URL %s", vcs.URL)
		}
	}
}
