package session

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/vmware/govmomi/vapi/tags"
)

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

func purgeCache() {
	sessionAnnotatedCache = map[string]*tagsAndCategoriesCache{}
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

		err := sessionObj.WithCachingTagsManager(context.TODO(), func(m *CachingTagsManager) error {
			createTagsAndCategories(ctx, tagsToCreate, categoriesToCreate, m, g)
			defer cleanupTagsAndCategories(ctx, m, g)
			defer purgeCache()

			g.Expect(len(sessionAnnotatedCache)).To(BeZero())

			tag, err := m.GetTag(ctx, "baz")
			g.Expect(err).To(Succeed())
			g.Expect(tag).NotTo(BeNil())

			// check cache filled
			g.Expect(len(sessionAnnotatedCache)).To(BeEquivalentTo(1))
			cachedTagId, found := getOrCreateSessionCache(sessionObj.sessionKey).tags.Get("baz")
			g.Expect(found).To(BeTrue())
			g.Expect(cachedTagId).Should(ContainSubstring("urn:"))

			return nil
		})
		g.Expect(err).Should(Succeed())
	})

	t.Run("Tag deleted from vCenter after being cached", func(t *testing.T) {
		g := NewWithT(t)
		tagsToCreate := []string{"fooo", "bar", "baz", "fizz"}
		categoriesToCreate := []string{"fizz"}

		err := sessionObj.WithCachingTagsManager(context.TODO(), func(m *CachingTagsManager) error {
			createTagsAndCategories(ctx, tagsToCreate, categoriesToCreate, m, g)
			defer purgeCache()

			g.Expect(len(sessionAnnotatedCache)).To(BeZero())

			tag, err := m.GetTag(ctx, "fizz")
			g.Expect(err).To(Succeed())
			g.Expect(tag).NotTo(BeNil())

			// check cache filled
			g.Expect(len(sessionAnnotatedCache)).To(BeEquivalentTo(1))
			cachedTagId, found := getOrCreateSessionCache(sessionObj.sessionKey).tags.Get("fizz")
			g.Expect(found).To(BeTrue())
			g.Expect(cachedTagId).Should(ContainSubstring("urn:"))

			cleanupTagsAndCategories(ctx, m, g)

			_, err = m.GetTag(ctx, "fizz")
			// Cache should be invalidated and not found err returned
			g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
			_, found = getOrCreateSessionCache(sessionObj.sessionKey).tags.Get("fizz")
			g.Expect(found).To(BeFalse())

			_, err = m.GetTag(ctx, "fizz")
			// Not found value should be landed to the cache after next call
			g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
			cachedTagId, found = getOrCreateSessionCache(sessionObj.sessionKey).tags.Get("fizz")
			g.Expect(found).To(BeTrue())
			g.Expect(cachedTagId).To(BeEquivalentTo(notFoundValue))

			return nil
		})
		g.Expect(err).Should(Succeed())
	})

	t.Run("Tag not found", func(t *testing.T) {
		g := NewWithT(t)

		err := sessionObj.WithCachingTagsManager(context.TODO(), func(m *CachingTagsManager) error {
			defer purgeCache()

			g.Expect(len(sessionAnnotatedCache)).To(BeZero())

			_, err := m.GetTag(ctx, "fizz")
			g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
			cachedTagId, found := getOrCreateSessionCache(sessionObj.sessionKey).tags.Get("fizz")
			g.Expect(found).To(BeTrue())
			g.Expect(cachedTagId).To(BeEquivalentTo(notFoundValue))

			return nil
		})
		g.Expect(err).Should(Succeed())
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

		err := sessionObj.WithCachingTagsManager(context.TODO(), func(m *CachingTagsManager) error {
			createTagsAndCategories(ctx, tagsToCreate, categoriesToCreate, m, g)
			defer cleanupTagsAndCategories(ctx, m, g)
			defer purgeCache()

			g.Expect(len(sessionAnnotatedCache)).To(BeZero())

			cat, err := m.GetCategory(ctx, "fizz")
			g.Expect(err).To(Succeed())
			g.Expect(cat).NotTo(BeNil())

			// check cache filled
			g.Expect(len(sessionAnnotatedCache)).To(BeEquivalentTo(1))
			cachedCategoryId, found := getOrCreateSessionCache(sessionObj.sessionKey).categories.Get("fizz")
			g.Expect(found).To(BeTrue())
			g.Expect(cachedCategoryId).Should(ContainSubstring("urn:"))

			return nil
		})
		g.Expect(err).Should(Succeed())
	})

	t.Run("Category deleted from vCenter after being cached", func(t *testing.T) {
		g := NewWithT(t)
		tagsToCreate := []string{"fooo", "bar"}
		categoriesToCreate := []string{"fizz", "bazz", "eggz"}

		err := sessionObj.WithCachingTagsManager(context.TODO(), func(m *CachingTagsManager) error {
			createTagsAndCategories(ctx, tagsToCreate, categoriesToCreate, m, g)
			defer purgeCache()

			g.Expect(len(sessionAnnotatedCache)).To(BeZero())

			cat, err := m.GetCategory(ctx, "fizz")
			g.Expect(err).To(Succeed())
			g.Expect(cat).NotTo(BeNil())

			// check cache filled
			g.Expect(len(sessionAnnotatedCache)).To(BeEquivalentTo(1))
			cachedCatId, found := getOrCreateSessionCache(sessionObj.sessionKey).categories.Get("fizz")
			g.Expect(found).To(BeTrue())
			g.Expect(cachedCatId).Should(ContainSubstring("urn:"))

			cleanupTagsAndCategories(ctx, m, g)

			_, err = m.GetCategory(ctx, "fizz")
			// Cache should be invalidated and not found err returned
			g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
			_, found = getOrCreateSessionCache(sessionObj.sessionKey).categories.Get("fizz")
			g.Expect(found).To(BeFalse())

			_, err = m.GetCategory(ctx, "fizz")
			// Not found value should be landed to the cache after next call
			g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
			cachedCatId, found = getOrCreateSessionCache(sessionObj.sessionKey).categories.Get("fizz")
			g.Expect(found).To(BeTrue())
			g.Expect(cachedCatId).To(BeEquivalentTo(notFoundValue))

			return nil
		})
		g.Expect(err).Should(Succeed())
	})

	t.Run("Category not found", func(t *testing.T) {
		g := NewWithT(t)

		err := sessionObj.WithCachingTagsManager(context.TODO(), func(m *CachingTagsManager) error {
			defer purgeCache()

			g.Expect(len(sessionAnnotatedCache)).To(BeZero())

			_, err := m.GetCategory(ctx, "fizz")
			g.Expect(err.Error()).To(ContainSubstring("404 Not Found"))
			cachedTagId, found := getOrCreateSessionCache(sessionObj.sessionKey).categories.Get("fizz")
			g.Expect(found).To(BeTrue())
			g.Expect(cachedTagId).To(BeEquivalentTo(notFoundValue))

			return nil
		})
		g.Expect(err).Should(Succeed())
	})

}

func TestSessionCacheGetter(t *testing.T) {
	defer purgeCache()

	g := NewWithT(t)
	g.Expect(len(sessionAnnotatedCache)).To(BeZero())

	getOrCreateSessionCache("foo")
	g.Expect(len(sessionAnnotatedCache)).To(BeEquivalentTo(1))
	getOrCreateSessionCache("foo")
	g.Expect(len(sessionAnnotatedCache)).To(BeEquivalentTo(1))

	getOrCreateSessionCache("bar")
	g.Expect(len(sessionAnnotatedCache)).To(BeEquivalentTo(2))
	getOrCreateSessionCache("bar")
	g.Expect(len(sessionAnnotatedCache)).To(BeEquivalentTo(2))
}

func TestValuesExpiration(t *testing.T) {
	defer purgeCache()

	g := NewWithT(t)
	g.Expect(len(sessionAnnotatedCache)).To(BeZero())

	cache := getOrCreateSessionCache("foo")
	cache.tags.SetWithTTL("foo", "bar", time.Millisecond*15)
	cache.tags.SetWithTTL("baz", "eggz", time.Second*15)

	value, found := cache.tags.Get("foo")
	g.Expect(found).To(BeTrue())
	g.Expect(value).To(BeEquivalentTo("bar"))

	g.Eventually(func() (found bool) {
		_, found = cache.tags.Get("foo")
		return found
	}, "20ms", "5ms").Should(BeFalse())

	g.Consistently(func() (found bool) {
		_, found = cache.tags.Get("baz")
		return found
	}, "20ms", "5ms").Should(BeTrue())
}
