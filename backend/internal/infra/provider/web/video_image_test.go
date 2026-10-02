package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
	infraegress "github.com/chenyme/grok2api/backend/internal/infra/egress"
	"github.com/chenyme/grok2api/backend/internal/infra/provider"
	"github.com/chenyme/grok2api/backend/internal/infra/security"
)

func TestImageVideoUsesUploadedMetadataAndSameAccount(t *testing.T) {
	for _, test := range []struct {
		name, uploadResponse string
		status               int
		generated            bool
	}{
		{"metadata", `{"fileMetadata":{"fileMetadataId":"first-frame-asset"}}`, 200, true},
		{"task ID is not asset", `{"uploadId":"unfinished-upload"}`, 200, false},
		{"upload denied", `{"error":{"code":"content-moderated"}}`, 403, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			uploads, generations := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.Header.Get("Cookie"), "test-video-sso") {
					t.Error("same account SSO missing")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/http/upload-file-v2/direct":
					uploads++
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Fatal(err)
					}
					f, _, err := r.FormFile("file")
					if err != nil {
						t.Fatal(err)
					}
					f.Close()
					if r.FormValue("file_source") != imagineSelfUploadSource {
						t.Error("wrong source")
					}
					w.WriteHeader(test.status)
					io.WriteString(w, test.uploadResponse)
				case "/rest/app-chat/conversations/new":
					generations++
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					input := nestedMap(payload, "mediaGenInput")
					if input["textToVideo"] != nil || input["referenceToVideo"] != nil {
						t.Error("first-frame semantics lost")
					}
					image := nestedMap(input, "imageToVideo")
					assets, _ := image["inputAssets"].([]any)
					if len(assets) != 1 || assets[0] != "first-frame-asset" || image["duration"] != float64(6) || image["resolutionName"] != "480p" || image["mode"] != "custom" {
						t.Errorf("wrong first-frame payload: %#v", image)
					}
					io.WriteString(w, `{"result":{"response":{"streamingVideoGenerationResponse":{"progress":100,"videoUrl":"users/u/generated/v/result.mp4"}}}}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			cipher, err := security.NewCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
			if err != nil {
				t.Fatal(err)
			}
			token, err := cipher.Encrypt("test-video-sso")
			if err != nil {
				t.Fatal(err)
			}
			adapter := NewAdapter(Config{BaseURL: server.URL, StatsigMode: "manual", StatsigManualValue: "test"}, infraegress.NewManager(egressRepositoryStub{}, cipher), cipher, nil, nil)
			png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="
			result, err := adapter.GenerateVideo(context.Background(), provider.VideoRequest{Credential: account.Credential{ID: 9, Provider: account.ProviderWeb, EncryptedAccessToken: token, WebTier: account.WebTierHeavy}, ImageURL: "data:image/png;base64," + png, Prompt: "Move gently", Duration: 6, Resolution: "480p"})
			if uploads != 1 {
				t.Errorf("uploads=%d", uploads)
			}
			if test.generated {
				if err != nil || result.URL == "" || generations != 1 {
					t.Fatalf("generation %d result=%#v error=%v", generations, result, err)
				}
			} else {
				if err == nil || generations != 0 {
					t.Fatalf("generation after upload failure=%d error=%v", generations, err)
				}
				if stage, ok := provider.VideoErrorStage(err); !ok || stage != provider.VideoStagePrepare {
					t.Fatalf("wrong failure stage %v", err)
				}
			}
		})
	}
}
