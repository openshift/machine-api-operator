package session

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/vmware/govmomi/vapi/tags"
)

func requireNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func createTagsAndCategories(ctx context.Context, tagNames []string, categoryNames []string, m *CachingTagsManager, g Gomega) {
	testCategoryID, err := m.CreateCategory(ctx, &tags.Category{
		AssociableTypes: []string{"VirtualMachine"},
		Cardinality:     "SINGLE",
		Name:            "test",
	})
	g.Expect(err).To(Succeed())
	for _, tagName := range tagNames {
		_, err := m.CreateTag(ctx, &tags.Tag{Name: tagName, CategoryID: testCategoryID})
		g.Expect(err).To(Succeed())
	}
	for _, catName := range categoryNames {
		_, err := m.CreateCategory(ctx, &tags.Category{
			AssociableTypes: []string{"VirtualMachine"},
			Cardinality:     "SINGLE",
			Name:            catName,
		})
		g.Expect(err).To(Succeed())
	}
}

func cleanupTagsAndCategories(ctx context.Context, m *CachingTagsManager, g Gomega) {
	tagsList, err := m.ListTags(ctx)
	g.Expect(err).To(Succeed())
	for _, tagID := range tagsList {
		g.Expect(m.DeleteTag(ctx, &tags.Tag{ID: tagID})).To(Succeed())
	}
	categoriesList, err := m.GetCategories(ctx)
	g.Expect(err).To(Succeed())
	for _, catID := range categoriesList {
		g.Expect(m.DeleteCategory(ctx, &tags.Category{ID: catID.ID})).To(Succeed())
	}
}

func TestGetTag(t *testing.T) {
	model, sessionObj, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	ctx := context.TODO()

	t.Run("Tag Found", func(t *testing.T) {
		g := NewWithT(t)
		tagsToCreate := []string{"fooo", "bar", "baz", "fizz"}
		categoriesToCreate := []string{"fizz"}

		m := newTagsCachingClient(sessionObj.TagManager)

		createTagsAndCategories(ctx, tagsToCreate, categoriesToCreate, m, g)
		defer cleanupTagsAndCategories(ctx, m, g)

		tag, err := m.GetTag(ctx, "baz")
		g.Expect(err).To(Succeed())
		g.Expect(tag).NotTo(BeNil())

		// check cache filled
		cachedTagId, found := m.tags.Get("baz")
		g.Expect(found).To(BeTrue())
		g.Expect(cachedTagId).Should(ContainSubstring("urn:"))
	})

	t.Run("Tag deleted from vCenter after being cached", func(t *testing.T) {
		g := NewWithT(t)
		tagsToCreate := []string{"fizz"}
		categoriesToCreate := []string{"fizz"}

		m := newTagsCachingClient(sessionObj.TagManager)

		createTagsAndCategories(ctx, tagsToCreate, categoriesToCreate, m, g)

		tag, err := m.GetTag(ctx, "fizz")
		g.Expect(err).To(Succeed())
		g.Expect(tag).NotTo(BeNil())

		// check cache filled
		cachedTagId, found := m.tags.Get("fizz")
		g.Expect(found).To(BeTrue())
		g.Expect(cachedTagId).Should(ContainSubstring("urn:"))

		cleanupTagsAndCategories(ctx, m, g)

		// A fresh manager simulates an expired object cache: the name->id cache hit
		// resolves to a stale id, the id lookup 404s, the name cache entry is
		// invalidated and lookup falls back to the default by-name search.
		m2 := newTagsCachingClient(sessionObj.TagManager)
		m2.tags.Set("fizz", tag.ID)

		_, err = m2.GetTag(ctx, "fizz")
		// Cache should be invalidated and not found err returned
		g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
		_, found = m2.tags.Get("fizz")
		g.Expect(found).To(BeFalse())

		_, err = m2.GetTag(ctx, "fizz")
		// Not found value should be landed to the cache after next call
		g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
		cachedTagId, found = m2.tags.Get("fizz")
		g.Expect(found).To(BeTrue())
		g.Expect(cachedTagId).To(BeEquivalentTo(notFoundValue))
	})

	t.Run("Tag 404 fallback success refills cache", func(t *testing.T) {
		g := NewWithT(t)
		m := newTagsCachingClient(sessionObj.TagManager)

		createTagsAndCategories(ctx, []string{"fizz"}, []string{"fizz"}, m, g)
		defer cleanupTagsAndCategories(ctx, m, g)

		tag, err := m.GetTag(ctx, "fizz")
		g.Expect(err).To(Succeed())

		// A fresh manager simulates an expired object cache holding a stale id:
		// the id lookup 404s and the by-name fallback finds the tag again.
		m2 := newTagsCachingClient(sessionObj.TagManager)
		m2.tags.Set("fizz", "urn:stale")

		tag2, err := m2.GetTag(ctx, "fizz")
		g.Expect(err).To(Succeed())
		g.Expect(tag2.ID).To(BeEquivalentTo(tag.ID))

		// Fallback success must refill both caches so the next call hits
		cachedID, found := m2.tags.Get("fizz")
		g.Expect(found).To(BeTrue())
		g.Expect(cachedID).To(BeEquivalentTo(tag.ID))
		obj, found := m2.tagObjects.Get(tag.ID)
		g.Expect(found).To(BeTrue())
		g.Expect(obj.(*tags.Tag).ID).To(BeEquivalentTo(tag.ID))
	})

	t.Run("Tag not found", func(t *testing.T) {
		g := NewWithT(t)

		m := newTagsCachingClient(sessionObj.TagManager)

		_, err := m.GetTag(ctx, "fizz")
		g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
		cachedTagId, found := m.tags.Get("fizz")
		g.Expect(found).To(BeTrue())
		g.Expect(cachedTagId).To(BeEquivalentTo(notFoundValue))

		// Second lookup is served from the not-found cache value
		_, err = m.GetTag(ctx, "fizz")
		g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
	})

}

func TestGetCategory(t *testing.T) {
	model, sessionObj, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	ctx := context.TODO()

	t.Run("Category Found", func(t *testing.T) {
		g := NewWithT(t)
		tagsToCreate := []string{"fooo"}
		categoriesToCreate := []string{"fizz", "bazz", "eggz"}

		m := newTagsCachingClient(sessionObj.TagManager)

		createTagsAndCategories(ctx, tagsToCreate, categoriesToCreate, m, g)
		defer cleanupTagsAndCategories(ctx, m, g)

		cat, err := m.GetCategory(ctx, "fizz")
		g.Expect(err).To(Succeed())
		g.Expect(cat).NotTo(BeNil())

		// check cache filled
		cachedCategoryId, found := m.categories.Get("fizz")
		g.Expect(found).To(BeTrue())
		g.Expect(cachedCategoryId).Should(ContainSubstring("urn:"))
	})

	t.Run("Category deleted from vCenter after being cached", func(t *testing.T) {
		g := NewWithT(t)
		tagsToCreate := []string{"fizz"}
		categoriesToCreate := []string{"fizz", "bazz", "eggz"}

		m := newTagsCachingClient(sessionObj.TagManager)

		createTagsAndCategories(ctx, tagsToCreate, categoriesToCreate, m, g)

		cat, err := m.GetCategory(ctx, "fizz")
		g.Expect(err).To(Succeed())
		g.Expect(cat).NotTo(BeNil())

		// check cache filled
		cachedCatId, found := m.categories.Get("fizz")
		g.Expect(found).To(BeTrue())
		g.Expect(cachedCatId).Should(ContainSubstring("urn:"))

		cleanupTagsAndCategories(ctx, m, g)

		// A fresh manager simulates an expired object cache: the name->id cache hit
		// resolves to a stale id, the id lookup 404s, the name cache entry is
		// invalidated and lookup falls back to the default by-name search.
		m2 := newTagsCachingClient(sessionObj.TagManager)
		m2.categories.Set("fizz", cat.ID)

		_, err = m2.GetCategory(ctx, "fizz")
		// Cache should be invalidated and not found err returned
		g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
		_, found = m2.categories.Get("fizz")
		g.Expect(found).To(BeFalse())

		_, err = m2.GetCategory(ctx, "fizz")
		// Not found value should be landed to the cache after next call
		g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
		cachedCatId, found = m2.categories.Get("fizz")
		g.Expect(found).To(BeTrue())
		g.Expect(cachedCatId).To(BeEquivalentTo(notFoundValue))
	})

	t.Run("Category 404 fallback success refills cache", func(t *testing.T) {
		g := NewWithT(t)
		m := newTagsCachingClient(sessionObj.TagManager)

		createTagsAndCategories(ctx, []string{"fizz"}, []string{"fizz", "bazz"}, m, g)
		defer cleanupTagsAndCategories(ctx, m, g)

		cat, err := m.GetCategory(ctx, "fizz")
		g.Expect(err).To(Succeed())

		// A fresh manager simulates an expired object cache holding a stale id:
		// the id lookup 404s and the by-name fallback finds the category again.
		m2 := newTagsCachingClient(sessionObj.TagManager)
		m2.categories.Set("fizz", "urn:stale")

		cat2, err := m2.GetCategory(ctx, "fizz")
		g.Expect(err).To(Succeed())
		g.Expect(cat2.ID).To(BeEquivalentTo(cat.ID))

		// Fallback success must refill both caches so the next call hits
		cachedID, found := m2.categories.Get("fizz")
		g.Expect(found).To(BeTrue())
		g.Expect(cachedID).To(BeEquivalentTo(cat.ID))
		obj, found := m2.categoryObjects.Get(cat.ID)
		g.Expect(found).To(BeTrue())
		g.Expect(obj.(*tags.Category).ID).To(BeEquivalentTo(cat.ID))
	})

	t.Run("Category not found", func(t *testing.T) {
		g := NewWithT(t)

		m := newTagsCachingClient(sessionObj.TagManager)

		_, err := m.GetCategory(ctx, "fizz")
		g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
		cachedTagId, found := m.categories.Get("fizz")
		g.Expect(found).To(BeTrue())
		g.Expect(cachedTagId).To(BeEquivalentTo(notFoundValue))

		// Second lookup is served from the not-found cache value
		_, err = m.GetCategory(ctx, "fizz")
		g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
	})

}

// objectCacheHit: a second fetch by the same ID (name or urn) must not
// re-fetch from the REST manager. We assert via pointer equality of the
// returned object, which only holds if the cached *tags.Tag is reused.
func TestGetTagObjectCache(t *testing.T) {
	model, sessionObj, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	g := NewWithT(t)
	m := newTagsCachingClient(sessionObj.TagManager)
	ctx := context.Background()

	createTagsAndCategories(ctx, []string{"cache-tag"}, []string{"cache-cat"}, m, g)
	defer cleanupTagsAndCategories(ctx, m, g)

	byName, err := m.GetTag(ctx, "cache-tag")
	requireNoErr(t, err)

	byID, err := m.GetTag(ctx, byName.ID)
	requireNoErr(t, err)

	if byName != byID {
		t.Fatalf("expected cached object reuse, got %p vs %p", byName, byID)
	}
}

func TestGetCategoryObjectCache(t *testing.T) {
	model, sessionObj, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	g := NewWithT(t)
	m := newTagsCachingClient(sessionObj.TagManager)
	ctx := context.Background()

	createTagsAndCategories(ctx, []string{"cache-tag"}, []string{"cache-cat"}, m, g)
	defer cleanupTagsAndCategories(ctx, m, g)

	byName, err := m.GetCategory(ctx, "cache-cat")
	requireNoErr(t, err)

	byID, err := m.GetCategory(ctx, byName.ID)
	requireNoErr(t, err)

	if byName != byID {
		t.Fatalf("expected cached object reuse, got %p vs %p", byName, byID)
	}
}

func TestValuesExpiration(t *testing.T) {
	g := NewWithT(t)

	cache := &objectCacheMap{}
	// 250ms is long enough that CI scheduling jitter cannot race the
	// immediately-following Get, yet short enough to expire quickly.
	cache.SetWithTTL("foo", "bar", 250*time.Millisecond)
	cache.SetWithTTL("baz", "eggz", time.Second*15)

	value, found := cache.Get("foo")
	g.Expect(found).To(BeTrue())
	g.Expect(value).To(BeEquivalentTo("bar"))

	// foo expires after ~250ms; give plenty of headroom for slow CI.
	g.Eventually(func() (found bool) {
		_, found = cache.Get("foo")
		return found
	}, "2s", "100ms").Should(BeFalse())

	// baz (15s TTL) stays present throughout this window.
	g.Consistently(func() (found bool) {
		_, found = cache.Get("baz")
		return found
	}, "500ms", "100ms").Should(BeTrue())
}

// TestGetTagRecreatedSameName verifies that a by-name lookup recovers from a
// stale cache entry when vCenter deletes a cached tag and recreates it under
// the same name with a new ID. Before the fix, the name-keyed object-cache hit
// short-circuited the idCache not-found recovery and returned the stale object.
func TestGetTagRecreatedSameName(t *testing.T) {
	ctx := context.TODO()
	model, sessionObj, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()
	g := NewWithT(t)

	m := newTagsCachingClient(sessionObj.TagManager)

	cat := &tags.Category{
		AssociableTypes: []string{"VirtualMachine"},
		Cardinality:     "SINGLE",
		Name:            "recreate-cat",
	}
	catID, err := m.CreateCategory(ctx, cat)
	requireNoErr(t, err)
	tagName := "recreate-tag"
	oldID, err := m.CreateTag(ctx, &tags.Tag{CategoryID: catID, Name: tagName})
	requireNoErr(t, err)

	// Prime the caches (both name->id and name/id->object) with a by-name lookup.
	got, err := m.GetTag(ctx, tagName)
	requireNoErr(t, err)
	g.Expect(got.ID).To(Equal(oldID))

	// Delete the tag and recreate it under the same name -> vCenter assigns a new ID.
	requireNoErr(t, m.DeleteTag(ctx, &tags.Tag{ID: oldID}))
	newID, err := m.CreateTag(ctx, &tags.Tag{CategoryID: catID, Name: tagName})
	requireNoErr(t, err)
	g.Expect(newID).NotTo(Equal(oldID))

	// The object-cache entry for the old ID is now stale. Simulate its
	// validation window lapsing (in production that happens after
	// objectValidationTTL); the name->id mapping is still cached, so the next
	// by-name lookup re-validates, sees the stale ID 404, and recovers.
	m.tagObjects.Delete(oldID)

	// A by-name lookup must return the recreated tag, not the stale cached object.
	got, err = m.GetTag(ctx, tagName)
	requireNoErr(t, err)
	g.Expect(got.ID).To(Equal(newID))
}

func TestObjectValuesExpiration(t *testing.T) {
	g := NewWithT(t)

	cache := &objectCacheMap{}
	// 250ms is long enough that CI scheduling jitter cannot race the
	// immediately-following Get, yet short enough to expire quickly.
	cache.SetWithTTL("foo", "bar", 250*time.Millisecond)
	cache.SetWithTTL("baz", "eggz", time.Second*15)

	obj, found := cache.Get("foo")
	g.Expect(found).To(BeTrue())
	g.Expect(obj).To(BeEquivalentTo("bar"))

	// foo expires after ~250ms; give plenty of headroom for slow CI.
	g.Eventually(func() (found bool) {
		_, found = cache.Get("foo")
		return found
	}, "2s", "100ms").Should(BeFalse())

	// baz (15s TTL) stays present throughout this window.
	g.Consistently(func() (found bool) {
		_, found = cache.Get("baz")
		return found
	}, "500ms", "100ms").Should(BeTrue())
}
