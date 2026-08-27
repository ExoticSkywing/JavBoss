package db

import (
	"context"
	"testing"

	"javboss/internal/jav"
	"javboss/internal/models"
)

func TestSaveJavInfoCanonicalizesParenthesizedActressAndBindsJavDBIdentity(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	if _, err := SaveJavInfo(ctx, &jav.JavInfo{
		Code:            "IDOL-001",
		Title:           "First work",
		Actors:          []string{"和葉みれい（藤白まき）"},
		ActorIdentities: map[string]string{"和葉みれい（藤白まき）": "actor-1"},
		Provider:        jav.ProviderJavDB,
	}); err != nil {
		t.Fatalf("save first work: %v", err)
	}
	if _, err := SaveJavInfo(ctx, &jav.JavInfo{
		Code:            "IDOL-002",
		Title:           "Second work",
		Actors:          []string{"藤白まき"},
		ActorIdentities: map[string]string{"藤白まき": "actor-1"},
		Provider:        jav.ProviderJavDB,
	}); err != nil {
		t.Fatalf("save second work: %v", err)
	}

	var idols []models.JavIdol
	if err := database.Find(&idols).Error; err != nil {
		t.Fatalf("list idols: %v", err)
	}
	if len(idols) != 1 || idols[0].Name != "藤白まき" || idols[0].JapaneseName != "藤白まき" {
		t.Fatalf("idols = %#v", idols)
	}
	var aliases []models.JavIdolAlias
	if err := database.Find(&aliases).Error; err != nil {
		t.Fatalf("list aliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].Alias != "和葉みれい" || aliases[0].JavIdolID != idols[0].ID {
		t.Fatalf("aliases = %#v", aliases)
	}
	var identities []models.JavIdolExternalIdentity
	if err := database.Find(&identities).Error; err != nil {
		t.Fatalf("list external identities: %v", err)
	}
	if len(identities) != 1 || identities[0].ExternalID != "actor-1" || identities[0].JavIdolID != idols[0].ID {
		t.Fatalf("identities = %#v", identities)
	}
	var mapCount int64
	if err := database.Model(&models.JavIdolMap{}).Where("jav_idol_id = ?", idols[0].ID).Count(&mapCount).Error; err != nil {
		t.Fatalf("count idol maps: %v", err)
	}
	if mapCount != 2 {
		t.Fatalf("idol map count = %d, want 2", mapCount)
	}
}

func TestNormalizeJavIdolIdentitiesChoosesJapaneseNameAndRedirectsSource(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	legacy := models.JavIdol{Name: "Old Alias", JapaneseName: "和葉みれい"}
	canonical := models.JavIdol{Name: "和葉みれい", JapaneseName: "和葉みれい"}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatalf("create legacy idol: %v", err)
	}
	if err := database.Create(&canonical).Error; err != nil {
		t.Fatalf("create canonical idol: %v", err)
	}
	work := models.Jav{Code: "IDOL-MERGE-001", Title: "Merge work"}
	if err := database.Create(&work).Error; err != nil {
		t.Fatalf("create work: %v", err)
	}
	if err := database.Create(&models.JavIdolMap{JavID: work.ID, JavIdolID: legacy.ID}).Error; err != nil {
		t.Fatalf("create idol map: %v", err)
	}

	merged, err := NormalizeJavIdolIdentities(ctx)
	if err != nil {
		t.Fatalf("normalize idols: %v", err)
	}
	if merged != 1 {
		t.Fatalf("merged count = %d, want 1", merged)
	}
	var idolCount int64
	if err := database.Model(&models.JavIdol{}).Count(&idolCount).Error; err != nil {
		t.Fatalf("count idols: %v", err)
	}
	if idolCount != 1 {
		t.Fatalf("idol count = %d, want 1", idolCount)
	}
	resolved, err := ResolveJavIdolID(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("resolve legacy idol: %v", err)
	}
	if resolved != canonical.ID {
		t.Fatalf("resolved id = %d, want %d", resolved, canonical.ID)
	}
	var mapRow models.JavIdolMap
	if err := database.Where("jav_id = ?", work.ID).First(&mapRow).Error; err != nil {
		t.Fatalf("load moved map: %v", err)
	}
	if mapRow.JavIdolID != canonical.ID {
		t.Fatalf("moved map idol = %d, want %d", mapRow.JavIdolID, canonical.ID)
	}
}

func TestReconcileJavDBIdolsMergesDifferentNamesWithSameExternalIdentity(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	first := models.JavIdol{Name: "和葉みれい", JapaneseName: "和葉みれい"}
	second := models.JavIdol{Name: "藤白まき", JapaneseName: "藤白まき"}
	if err := database.Create(&first).Error; err != nil {
		t.Fatalf("create first idol: %v", err)
	}
	if err := database.Create(&second).Error; err != nil {
		t.Fatalf("create second idol: %v", err)
	}
	works := []models.Jav{
		{Code: "JDB-ID-001", NormalizedCode: models.NormalizeJavCode("JDB-ID-001"), Title: "First"},
		{Code: "JDB-ID-002", NormalizedCode: models.NormalizeJavCode("JDB-ID-002"), Title: "Second"},
	}
	if err := database.Create(&works).Error; err != nil {
		t.Fatalf("create works: %v", err)
	}
	if err := database.Create(&[]models.JavIdolMap{
		{JavID: works[0].ID, JavIdolID: first.ID},
		{JavID: works[1].ID, JavIdolID: second.ID},
	}).Error; err != nil {
		t.Fatalf("create idol maps: %v", err)
	}

	for index, actorName := range []string{"和葉みれい", "藤白まき"} {
		if _, err := ReconcileJavDBIdols(ctx, works[index].ID, works[index].Code, &jav.JavInfo{
			Code:            works[index].Code,
			Actors:          []string{actorName},
			ActorIdentities: map[string]string{actorName: "same-javdb-actor"},
			Provider:        jav.ProviderJavDB,
		}); err != nil {
			t.Fatalf("reconcile work %d: %v", index, err)
		}
	}

	var idols []models.JavIdol
	if err := database.Find(&idols).Error; err != nil {
		t.Fatalf("list reconciled idols: %v", err)
	}
	if len(idols) != 1 || idols[0].Name != "藤白まき" || idols[0].JapaneseName != "藤白まき" {
		t.Fatalf("reconciled idols = %#v", idols)
	}
	var aliases []models.JavIdolAlias
	if err := database.Find(&aliases).Error; err != nil {
		t.Fatalf("list reconciled aliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].JavIdolID != idols[0].ID || aliases[0].Alias != "和葉みれい" {
		t.Fatalf("reconciled aliases = %#v", aliases)
	}
	var maps []models.JavIdolMap
	if err := database.Order("jav_id").Find(&maps).Error; err != nil {
		t.Fatalf("list reconciled maps: %v", err)
	}
	if len(maps) != 2 || maps[0].JavIdolID != idols[0].ID || maps[1].JavIdolID != idols[0].ID {
		t.Fatalf("reconciled maps = %#v", maps)
	}
	resolved, err := ResolveJavIdolID(ctx, second.ID)
	if err != nil {
		t.Fatalf("resolve merged idol: %v", err)
	}
	if resolved != idols[0].ID {
		t.Fatalf("resolved id = %d, want %d", resolved, idols[0].ID)
	}
}
