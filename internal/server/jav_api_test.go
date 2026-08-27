package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"javboss/internal/common"
	dbpkg "javboss/internal/db"
	"javboss/internal/jav"
	"javboss/internal/models"

	"github.com/gin-gonic/gin"
)

func TestParseJavFilterQueryInventory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		query      string
		want       string
		wantOK     bool
		wantStatus int
	}{
		{name: "default is unified collection", want: models.JavInventoryAll, wantOK: true},
		{name: "all", query: "?inventory=all", want: models.JavInventoryAll, wantOK: true},
		{name: "pending", query: "?inventory=pending", want: models.JavInventoryPending, wantOK: true},
		{name: "imported", query: "?inventory=imported", want: models.JavInventoryImported, wantOK: true},
		{name: "case insensitive", query: "?inventory=PENDING", want: models.JavInventoryPending, wantOK: true},
		{name: "invalid", query: "?inventory=stored", wantOK: false, wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/jav"+test.query, nil)

			got, ok := parseJavFilterQuery(context)
			if ok != test.wantOK {
				t.Fatalf("ok = %v, want %v; status=%d body=%s", ok, test.wantOK, recorder.Code, recorder.Body.String())
			}
			if test.wantOK && got.Inventory != test.want {
				t.Fatalf("inventory = %q, want %q", got.Inventory, test.want)
			}
			if !test.wantOK && recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
		})
	}
}

func TestSearchJavInventoryAPI(t *testing.T) {
	database, err := dbpkg.Open(filepath.Join(t.TempDir(), "jav-inventory-api.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	previousDB := common.DB
	common.DB = database
	t.Cleanup(func() {
		common.DB = previousDB
		if sqlDB, dbErr := database.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	pending := models.Jav{Code: "API-INV-001", Title: "Pending"}
	imported := models.Jav{Code: "API-INV-002", Title: "Imported"}
	if err := database.Create(&pending).Error; err != nil {
		t.Fatalf("create pending JAV: %v", err)
	}
	if err := database.Create(&imported).Error; err != nil {
		t.Fatalf("create imported JAV: %v", err)
	}
	if err := database.Create(&models.JavAcquisition{
		JavID: pending.ID,
		Stage: models.JavAcquisitionStageMetadataPending,
	}).Error; err != nil {
		t.Fatalf("create pending acquisition: %v", err)
	}
	directory := models.Directory{Path: "/media/inventory-api"}
	video := models.Video{Fingerprint: "inventory-api-video"}
	if err := database.Create(&directory).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := database.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	location, err := dbpkg.UpsertVideoLocation(
		context.Background(), video.ID, directory.ID, "API-INV-002.mp4", time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := dbpkg.SetVideoLocationJavIDForVideo(
		context.Background(), location.ID, video.ID, imported.ID, location.UpdatedAt,
	); err != nil {
		t.Fatalf("link imported JAV: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/jav", searchJav)
	type inventoryResponse struct {
		Items        []models.Jav `json:"items"`
		Total        int64        `json:"total"`
		PendingTotal int64        `json:"pending_total"`
	}
	request := func(query string) (inventoryResponse, *httptest.ResponseRecorder) {
		t.Helper()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/jav"+query, nil))
		var response inventoryResponse
		if recorder.Code == http.StatusOK {
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode inventory response: %v", err)
			}
		}
		return response, recorder
	}

	all, recorder := request("")
	if recorder.Code != http.StatusOK || all.Total != 2 || all.PendingTotal != 1 || len(all.Items) != 2 {
		t.Fatalf("all inventory status=%d response=%#v body=%s", recorder.Code, all, recorder.Body.String())
	}
	states := map[string]models.Jav{}
	for _, item := range all.Items {
		states[item.Code] = item
	}
	if states[pending.Code].InventoryState != models.JavInventoryPending ||
		states[pending.Code].AcquisitionStage != models.JavAcquisitionStageMetadataPending {
		t.Fatalf("pending API item = %#v", states[pending.Code])
	}
	if states[imported.Code].InventoryState != models.JavInventoryImported ||
		states[imported.Code].AcquisitionStage != models.JavAcquisitionStageImported {
		t.Fatalf("imported API item = %#v", states[imported.Code])
	}

	pendingOnly, recorder := request("?inventory=pending")
	if recorder.Code != http.StatusOK || pendingOnly.Total != 1 || pendingOnly.PendingTotal != 1 || len(pendingOnly.Items) != 1 || pendingOnly.Items[0].ID != pending.ID {
		t.Fatalf("pending inventory status=%d response=%#v", recorder.Code, pendingOnly)
	}
	importedOnly, recorder := request("?inventory=imported")
	if recorder.Code != http.StatusOK || importedOnly.Total != 1 || importedOnly.PendingTotal != 1 || len(importedOnly.Items) != 1 || importedOnly.Items[0].ID != imported.ID {
		t.Fatalf("imported inventory status=%d response=%#v", recorder.Code, importedOnly)
	}
	_, recorder = request("?inventory=stored")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid inventory status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateJavEditOptionsReturnsPersistentIDs(t *testing.T) {
	database, err := dbpkg.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	previousDB := common.DB
	common.DB = database
	t.Cleanup(func() {
		common.DB = previousDB
		if sqlDB, dbErr := database.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/jav/idols", createJavIdol)
	router.POST("/jav/tags/scraped", createJavScrapedTag)
	router.GET("/jav/tags", listJavTags)

	requestJSON := func(method, path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		return recorder
	}

	idolResponse := requestJSON(http.MethodPost, "/jav/idols", `{"name":"即時建立女优"}`)
	if idolResponse.Code != http.StatusCreated {
		t.Fatalf("create idol status = %d body=%s", idolResponse.Code, idolResponse.Body.String())
	}
	var idol dbpkg.JavIdolSummary
	if err := json.Unmarshal(idolResponse.Body.Bytes(), &idol); err != nil {
		t.Fatalf("decode idol response: %v", err)
	}
	if idol.ID <= 0 || idol.Name != "即時建立女优" {
		t.Fatalf("created idol = %#v", idol)
	}

	duplicateResponse := requestJSON(http.MethodPost, "/jav/idols", `{"name":"即時建立女优"}`)
	var duplicate dbpkg.JavIdolSummary
	if err := json.Unmarshal(duplicateResponse.Body.Bytes(), &duplicate); err != nil {
		t.Fatalf("decode duplicate idol response: %v", err)
	}
	if duplicateResponse.Code != http.StatusCreated || duplicate.ID != idol.ID {
		t.Fatalf("duplicate idol = %#v status=%d, want id %d", duplicate, duplicateResponse.Code, idol.ID)
	}

	tagResponse := requestJSON(http.MethodPost, "/jav/tags/scraped", `{"name":"无码"}`)
	if tagResponse.Code != http.StatusCreated {
		t.Fatalf("create scraped tag status = %d body=%s", tagResponse.Code, tagResponse.Body.String())
	}
	var tag dbpkg.JavTagCount
	if err := json.Unmarshal(tagResponse.Body.Bytes(), &tag); err != nil {
		t.Fatalf("decode scraped tag response: %v", err)
	}
	if tag.ID <= 0 || tag.Name != "無碼" || tag.Provider != int(jav.ProviderManualScrape) {
		t.Fatalf("created scraped tag = %#v", tag)
	}

	listResponse := requestJSON(http.MethodGet, "/jav/tags", "")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list tags status = %d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var tags []dbpkg.JavTagCount
	if err := json.Unmarshal(listResponse.Body.Bytes(), &tags); err != nil {
		t.Fatalf("decode tags response: %v", err)
	}
	if len(tags) != 1 || tags[0].ID != tag.ID || tags[0].Count != 0 {
		t.Fatalf("listed tags = %#v, want zero-count created tag %#v", tags, tag)
	}
}

func acceptJavSampleImageURL(_ context.Context, _ string) (bool, error) {
	return true, nil
}

func TestLookupJavSampleImagesByProviderFallsBackFromJavMenuToJavBus(t *testing.T) {
	var calls []jav.Provider
	images, err := lookupJavSampleImagesByProvider(context.Background(), "IPX-228", func(code string, provider jav.Provider) (*jav.JavInfo, error) {
		if code != "IPX-228" {
			t.Fatalf("unexpected code: %q", code)
		}
		calls = append(calls, provider)
		switch provider {
		case jav.ProviderJavMenu:
			return &jav.JavInfo{Code: code, Provider: provider}, nil
		case jav.ProviderJavBus:
			return &jav.JavInfo{
				Code:     code,
				Provider: provider,
				SampleImages: []jav.SampleImage{
					{
						ThumbnailURL: "https://example.com/thumb.jpg",
						DetailURL:    "https://example.com/detail.jpg",
					},
				},
			}, nil
		default:
			t.Fatalf("unexpected provider: %s", provider.String())
			return nil, nil
		}
	}, acceptJavSampleImageURL)
	if err != nil {
		t.Fatalf("lookup sample images: %v", err)
	}
	if !reflect.DeepEqual(calls, []jav.Provider{jav.ProviderJavMenu, jav.ProviderJavBus}) {
		t.Fatalf("provider calls = %#v", calls)
	}
	want := models.JavSampleImages{
		{
			ThumbnailURL: "https://example.com/thumb.jpg",
			DetailURL:    "https://example.com/detail.jpg",
		},
	}
	if !reflect.DeepEqual(images, want) {
		t.Fatalf("sample images = %#v, want %#v", images, want)
	}
}

func TestLookupJavSampleImagesByProviderStopsAfterJavMenuSuccess(t *testing.T) {
	var calls []jav.Provider
	images, err := lookupJavSampleImagesByProvider(context.Background(), "IPX-228", func(_ string, provider jav.Provider) (*jav.JavInfo, error) {
		calls = append(calls, provider)
		if provider != jav.ProviderJavMenu {
			return nil, errors.New("JavBus must not be called after JavMenu succeeds")
		}
		return &jav.JavInfo{
			SampleImages: []jav.SampleImage{
				{ThumbnailURL: "thumbnail", DetailURL: "detail"},
			},
		}, nil
	}, acceptJavSampleImageURL)
	if err != nil {
		t.Fatalf("lookup sample images: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("sample image count = %d, want 1", len(images))
	}
	if !reflect.DeepEqual(calls, []jav.Provider{jav.ProviderJavMenu}) {
		t.Fatalf("provider calls = %#v", calls)
	}
}

func TestLookupJavSampleImagesByProviderPreservesTemporaryErrors(t *testing.T) {
	temporaryErr := errors.New("network timeout")
	images, err := lookupJavSampleImagesByProvider(context.Background(), "IPX-228", func(_ string, provider jav.Provider) (*jav.JavInfo, error) {
		switch provider {
		case jav.ProviderJavMenu:
			return nil, temporaryErr
		case jav.ProviderJavBus:
			return nil, jav.ResourceNotFonud
		default:
			t.Fatalf("unexpected provider: %s", provider.String())
			return nil, nil
		}
	}, acceptJavSampleImageURL)
	if len(images) != 0 {
		t.Fatalf("sample image count = %d, want 0", len(images))
	}
	if !errors.Is(err, temporaryErr) {
		t.Fatalf("lookup error = %v, want network timeout", err)
	}
}

func TestLookupJavSampleImagesByProviderTreatsConfirmedMissAsNotFound(t *testing.T) {
	images, err := lookupJavSampleImagesByProvider(context.Background(), "IPX-228", func(_ string, _ jav.Provider) (*jav.JavInfo, error) {
		return nil, jav.ResourceNotFonud
	}, acceptJavSampleImageURL)
	if err != nil {
		t.Fatalf("confirmed miss returned error: %v", err)
	}
	if len(images) != 0 {
		t.Fatalf("sample image count = %d, want 0", len(images))
	}
}

func TestLookupJavSampleImagesByProviderValidatesLastDetailURLAndFallsBack(t *testing.T) {
	var calls []jav.Provider
	var validated []string
	images, err := lookupJavSampleImagesByProvider(
		context.Background(),
		"IPX-228",
		func(_ string, provider jav.Provider) (*jav.JavInfo, error) {
			calls = append(calls, provider)
			return &jav.JavInfo{SampleImages: []jav.SampleImage{
				{ThumbnailURL: "thumb-1", DetailURL: provider.String() + "-detail-1"},
				{ThumbnailURL: "thumb-2", DetailURL: provider.String() + "-detail-10"},
			}}, nil
		},
		func(_ context.Context, detailURL string) (bool, error) {
			validated = append(validated, detailURL)
			return strings.HasPrefix(detailURL, "javbus-"), nil
		},
	)
	if err != nil {
		t.Fatalf("lookup sample images: %v", err)
	}
	if !reflect.DeepEqual(calls, []jav.Provider{jav.ProviderJavMenu, jav.ProviderJavBus}) {
		t.Fatalf("provider calls = %#v", calls)
	}
	if !reflect.DeepEqual(validated, []string{"javmenu-detail-10", "javbus-detail-10"}) {
		t.Fatalf("validated URLs = %#v", validated)
	}
	if len(images) != 2 || images[1].DetailURL != "javbus-detail-10" {
		t.Fatalf("sample images = %#v", images)
	}
}

func TestValidateJavSampleImageDetailURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/valid.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00})
		case "/invalid.jpg":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not an image"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "valid image", url: server.URL + "/valid.jpg", want: true},
		{name: "HTML response", url: server.URL + "/invalid.jpg", want: false},
		{name: "missing image", url: server.URL + "/missing.jpg", want: false},
		{name: "invalid URL", url: "javascript:alert(1)", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateJavSampleImageDetailURL(context.Background(), test.url)
			if err != nil {
				t.Fatalf("validate detail URL: %v", err)
			}
			if got != test.want {
				t.Fatalf("valid = %v, want %v", got, test.want)
			}
		})
	}
}
