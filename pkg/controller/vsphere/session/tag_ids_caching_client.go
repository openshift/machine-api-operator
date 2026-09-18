package session

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vmware/govmomi/vapi/tags"

	"k8s.io/klog/v2"
)

const (
	defaultCacheTTL = time.Hour * 12

	// objectValidationTTL bounds how long a cached tag/category object is
	// trusted before a by-name lookup re-validates it with a REST fetch. This
	// lets a name->id cache hit reuse the cached object instead of fetching on
	// every lookup, while still catching an object that is deleted and
	// recreated under the same name (new ID) once the window lapses.
	objectValidationTTL = 15 * time.Minute

	// special value for determine whenever tag or category was found in vCenter during last lookup
	notFoundValue      = "NOT_FOUND"
	notFoundErrMessage = "404 Not Found"
)

type cachedObject struct {
	obj       any
	expiresAt int64
}

func (co cachedObject) expired() bool {
	return time.Now().UnixNano() > co.expiresAt
}

type objectCacheMap struct {
	internalMap sync.Map
}

// SetWithTTL stores the object by the given key with lifetime specified in ttl parameter.
func (c *objectCacheMap) SetWithTTL(key string, obj any, ttl time.Duration) {
	c.internalMap.Store(key, cachedObject{obj: obj, expiresAt: time.Now().Add(ttl).UnixNano()})
}

// Set stores the object with the default 12h lifetime.
func (c *objectCacheMap) Set(key string, obj any) {
	c.SetWithTTL(key, obj, defaultCacheTTL)
}

// Delete removes the cached value for key.
func (c *objectCacheMap) Delete(key string) {
	c.internalMap.Delete(key)
}

// Get returns the cached object. Second return value indicates a hit.
func (c *objectCacheMap) Get(key string) (any, bool) {
	item, found := c.internalMap.Load(key)
	if !found {
		return nil, false
	}
	co := item.(cachedObject)
	if co.expired() {
		klog.V(4).Infof("object cache item with key %s expired, invalidating", key)
		c.internalMap.Delete(key)
		return nil, false
	}
	return co.obj, true
}

// CachingTagsManager wraps tags.Manager from vSphere SDK for
// cache mapping between tags or categories name and their ids,
// and for caching the full tag/category objects themselves.
// Reasoning behind this is the implementation details of tags/categories lookup by name,
// to find a tag/category by name vSphere SDK gets a list of ids and then makes an additional request
// for every object till it will not find matched names. Such peculiarity causes a huge performance degradation
// within environments with a large number of tags/categories, because in the worst case number of rest requests for
// object lookup might be equal to the number of objects.
//
// See tags.Manager methods for more details: https://github.com/vmware/govmomi/blob/a2fb82dc55a8eb00932233aa8028ce97140df784/vapi/tags/tags.go#L172
//
// This structure is intended to be used from a Session instance (Session.GetCachingTagsManager method specifically) presented in this module.
type CachingTagsManager struct {
	*tags.Manager

	tags            objectCacheMap // name -> ID
	categories      objectCacheMap // name -> ID
	tagObjects      objectCacheMap // ID -> *tags.Tag
	categoryObjects objectCacheMap // ID -> *tags.Category
}

func newTagsCachingClient(tagsManager *tags.Manager) *CachingTagsManager {
	return &CachingTagsManager{Manager: tagsManager}
}

// IsName returns true if the id is not an urn.
// this method came from vSphere sdk,
// see https://github.com/vmware/govmomi/blob/a2fb82dc55a8eb00932233aa8028ce97140df784/vapi/tags/tags.go#L121 for
// more context.
func IsName(id string) bool {
	return !strings.HasPrefix(id, "urn:")
}

// isObjectNotFoundErr checks if error message contains "Not Found" message.
// vSphere api client does not expose error type, so we can rely only on error message
func isObjectNotFoundErr(err error) bool {
	return err != nil && strings.HasSuffix(err.Error(), http.StatusText(http.StatusNotFound))
}

// lookupObject implements the lookup flow shared by GetTag and GetCategory:
// object-cache hit -> name->id cache (notFoundValue sentinel, invalidation
// fallback when a cached id goes stale) -> fetch by id or name, refilling
// both caches on success.
//
// Not-found results are cached for 12 hours (defaultCacheTTL) because vCenter
// REST by-name lookups are expensive: for every missing object the number of
// requests equals the number of categories/tags. See govmomi vapi/tags and
// the vCenter REST API docs referenced in the original implementation.
//
// Object-cache entries live for objectValidationTTL. A name->id hit reuses the
// cached object while that window is open and re-validates with a REST fetch
// once it lapses, catching objects deleted and recreated under the same name.
func lookupObject[T any](
	ctx context.Context,
	kind string,
	objCache *objectCacheMap,
	idCache *objectCacheMap,
	fetchByID func(ctx context.Context, id string) (T, error),
	nameOf func(T) string,
	idOf func(T) string,
	id string,
) (T, error) {
	var zero T

	// Only serve object-cache hits for explicit IDs. A name-keyed object-cache
	// entry can go stale when vCenter deletes and recreates the object under the
	// same name (assigning a new ID); a name lookup that returned that stale
	// object would bypass the idCache not-found recovery below, so names are
	// routed through the idCache path instead.
	if !IsName(id) {
		if obj, ok := objCache.Get(id); ok {
			if o, ok := obj.(T); ok {
				return o, nil
			}
		}
		obj, err := fetchByID(ctx, id)
		if err == nil {
			objCache.SetWithTTL(id, obj, objectValidationTTL)
		}
		return obj, err
	}

	cachedIDValue, found := idCache.Get(id)
	if found {
		cachedID := cachedIDValue.(string)
		if cachedID == notFoundValue {
			klog.V(4).Infof("%s %s: cache indicates %s does not exist", kind, id, kind)
			return zero, fmt.Errorf("%s", notFoundErrMessage)
		}
		// Fast path: while the object's validation window is open, reuse the
		// cached object so a name->id hit does not issue a REST request.
		if obj, ok := objCache.Get(cachedID); ok {
			if o, ok := obj.(T); ok {
				return o, nil
			}
		}
		obj, err := fetchByID(ctx, cachedID)
		if err != nil {
			if isObjectNotFoundErr(err) {
				klog.V(3).Infof("%s %s: %s was not found in vCenter by cached id, invalidating cache", kind, id, kind)
				// if not found, invalidate the name->id cache and fallback to the by-name lookup
				idCache.Delete(id)
				obj, err = fetchByID(ctx, id)
				if err == nil {
					// fallback found it by name, refill both caches so the next call hits
					idCache.Set(nameOf(obj), idOf(obj))
					objCache.SetWithTTL(idOf(obj), obj, objectValidationTTL)
				}
			}
			return obj, err
		}
		// Validation fetch succeeded; refresh the object cache so the fast
		// path stays warm for the next validation window.
		objCache.SetWithTTL(cachedID, obj, objectValidationTTL)
		return obj, nil
	}

	klog.V(3).Infof("%s %s: %s cache miss, trying to find %s by name", kind, id, kind, kind)
	obj, err := fetchByID(ctx, id)
	if err != nil {
		if isObjectNotFoundErr(err) {
			klog.V(3).Infof("%s %s not found in vCenter, caching", kind, id)
			idCache.Set(id, notFoundValue)
		}
		return obj, err
	}
	idCache.Set(nameOf(obj), idOf(obj))
	objCache.SetWithTTL(idOf(obj), obj, objectValidationTTL)
	return obj, err
}

// GetTag fetches the tag information for the given identifier.
// The id parameter can be a Tag ID or Tag Name.
// This method shadows original tags.Manager method and caches mapping between
// tag name and its id as well as the full tag object, so any hit path
// (object cache or name->id cache) returns without REST calls.
//
// In case if a tag was not found in vCenter, this would be cached for 12 hours and lookup won't happen till cache expiration.
func (t *CachingTagsManager) GetTag(ctx context.Context, id string) (*tags.Tag, error) {
	return lookupObject(ctx, "tag", &t.tagObjects, &t.tags, t.Manager.GetTag,
		func(tag *tags.Tag) string { return tag.Name },
		func(tag *tags.Tag) string { return tag.ID },
		id)
}

// GetCategory fetches the category information for the given identifier.
// The id parameter can be a Category ID or Category Name.
// This method shadows original tags.Manager method and caches mapping between
// category name and its id as well as the full category object, so any hit path
// (object cache or name->id cache) returns without REST calls.
//
// In case if a category was not found in vCenter, this would be cached for 12 hours and lookup won't happen till cache expiration.
func (t *CachingTagsManager) GetCategory(ctx context.Context, id string) (*tags.Category, error) {
	return lookupObject(ctx, "category", &t.categoryObjects, &t.categories, t.Manager.GetCategory,
		func(category *tags.Category) string { return category.Name },
		func(category *tags.Category) string { return category.ID },
		id)
}

// ListTagsForCategory tag ids for the given category.
// The id parameter can be a Category ID or Category Name.
// Uses caching GetCategory method.
func (t *CachingTagsManager) ListTagsForCategory(ctx context.Context, id string) ([]string, error) {
	if IsName(id) {
		category, err := t.GetCategory(ctx, id)
		if err != nil {
			return nil, err
		}
		id = category.ID
	}
	return t.Manager.ListTagsForCategory(ctx, id)
}
