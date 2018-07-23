package diff

import (
	"fmt"
	"sort"
)

// Diff3MergeMaps performs a 3-way merge of a, b, and c, where a is the parent. It returns the merged
// results if there are no conflicts, or error if a conflict was encountered.
func Diff3MergeMaps(a, b, c map[string]Eq) (map[string]Eq, error) {
	return merge3maps(diff3maps(a, b, c))
}

// mhunk is a sum type for the types of changes in a 3-way merge.
type mhunk interface {
	kind() int
	append(string, chunk, chunk) mhunk
}

type unchangedKeys struct {
	chunks map[string]chunk
}

func (unchangedKeys) kind() int {
	return hunkUnchanged
}

func (u unchangedKeys) append(k string, c, _ chunk) mhunk {
	u.chunks[k] = c
	return u
}

type leftChangeKeys struct {
	chunks map[string]chunk
}

func (leftChangeKeys) kind() int {
	return hunkLeftChange
}

func (l leftChangeKeys) append(k string, c, _ chunk) mhunk {
	l.chunks[k] = c
	return l
}

type rightChangeKeys struct {
	chunks map[string]chunk
}

func (r rightChangeKeys) append(k string, _, c chunk) mhunk {
	r.chunks[k] = c
	return r
}

func (rightChangeKeys) kind() int {
	return hunkRightChange
}

type conflictKeys struct {
	left  map[string]chunk
	right map[string]chunk
}

func (conflictKeys) kind() int {
	return hunkConflict
}

func (c conflictKeys) append(k string, left, right chunk) mhunk {
	c.left[k] = left
	c.right[k] = right
	return c
}

type mhunkBuilder struct {
	mhunks []mhunk
}

func (mh *mhunkBuilder) appendLeftChange(k string, c chunk) {
	if len(mh.mhunks) == 0 || mh.mhunks[len(mh.mhunks)-1].kind() != hunkLeftChange {
		mh.mhunks = append(mh.mhunks, leftChangeKeys{chunks: map[string]chunk{k: c}})
	} else {
		mh.mhunks[len(mh.mhunks)-1] = mh.mhunks[len(mh.mhunks)-1].append(k, c, c)
	}
}

func (mh *mhunkBuilder) appendRightChange(k string, c chunk) {
	if len(mh.mhunks) == 0 || mh.mhunks[len(mh.mhunks)-1].kind() != hunkRightChange {
		mh.mhunks = append(mh.mhunks, rightChangeKeys{chunks: map[string]chunk{k: c}})
	} else {
		mh.mhunks[len(mh.mhunks)-1] = mh.mhunks[len(mh.mhunks)-1].append(k, c, c)
	}
}

func (mh *mhunkBuilder) appendUnchanged(k string, left, right chunk) {
	if len(mh.mhunks) == 0 || mh.mhunks[len(mh.mhunks)-1].kind() != hunkUnchanged {
		mh.mhunks = append(mh.mhunks, unchangedKeys{chunks: map[string]chunk{k: left}})
	} else {
		mh.mhunks[len(mh.mhunks)-1] = mh.mhunks[len(mh.mhunks)-1].append(k, left, right)
	}
}

func (mh *mhunkBuilder) appendConflict(k string, left, right chunk) {
	if len(mh.mhunks) == 0 || mh.mhunks[len(mh.mhunks)-1].kind() != hunkConflict {
		mh.mhunks = append(mh.mhunks, conflictKeys{left: map[string]chunk{k: left}, right: map[string]chunk{k: right}})
	} else {
		mh.mhunks[len(mh.mhunks)-1] = mh.mhunks[len(mh.mhunks)-1].append(k, left, right)
	}
}

// diff3maps implements a 3-way style diff on maps.
func diff3maps(a, b, c map[string]Eq) []mhunk {
	ab := diff2maps(a, b)
	ac := diff2maps(a, c)
	mhb := &mhunkBuilder{}
	// Create union of keys.
	keys := make(map[string]struct{})
	for k := range ab {
		keys[k] = struct{}{}
	}
	for k := range ac {
		keys[k] = struct{}{}
	}
	ka := []string{}
	for k := range keys {
		ka = append(ka, k)
	}
	sort.Strings(ka)

	for _, k := range ka {
		abVal, abOK := ab[k]
		acVal, acOK := ac[k]
		if !abOK && acOK {
			mhb.appendRightChange(k, acVal)
			continue
		}
		if abOK && !acOK {
			mhb.appendLeftChange(k, abVal)
			continue
		}

		switch {
		case abVal.kind() == acVal.kind() && abVal.val().Eq(acVal.val()):
			mhb.appendUnchanged(k, abVal, acVal)
		case ((abVal.kind() == chunkAdd && acVal.kind() == chunkChange) || (abVal.kind() == chunkChange && acVal.kind() == chunkAdd)) && abVal.val().Eq(acVal.val()):
			mhb.appendUnchanged(k, abVal, acVal)
		case abVal.kind() != chunkKeep && acVal.kind() == chunkKeep:
			mhb.appendLeftChange(k, abVal)
		case abVal.kind() == chunkKeep && acVal.kind() != chunkKeep:
			mhb.appendRightChange(k, acVal)
		default:
			mhb.appendConflict(k, abVal, acVal)
		}
	}
	return mhb.mhunks
}

// merge3maps takes a series of hunks and performs a 3-way-merge. It returns the merged sequence on success,
// or error if it encountered a conflict.
func merge3maps(mhunks []mhunk) (map[string]Eq, error) {
	merged := map[string]Eq{}
	for i, h := range mhunks {
		switch t := h.(type) {
		case leftChangeKeys:
			for k, c := range t.chunks {
				if c.kind() != chunkDel {
					merged[k] = c.val()
				}
			}
		case rightChangeKeys:
			for k, c := range t.chunks {
				if c.kind() != chunkDel {
					merged[k] = c.val()
				}
			}
		case unchangedKeys:
			for k, c := range t.chunks {
				if c.kind() != chunkDel {
					merged[k] = c.val()
				}
			}
		case conflictKeys:
			return nil, fmt.Errorf("encountered conflict at hunk %d (%v), cannot merge", i, h)
		}
	}
	return merged, nil
}

// diff2maps returns a series of chunks that represent the diff.
func diff2maps(a, b map[string]Eq) map[string]chunk {
	chunks := map[string]chunk{}
	// Create union of keys.
	keys := make(map[string]struct{})
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}

	for k := range keys {
		aVal, aOK := a[k]
		bVal, bOK := b[k]
		switch {
		case aOK && bOK:
			if aVal.Eq(bVal) {
				chunks[k] = keep{aVal}
			} else {
				chunks[k] = change{bVal}
			}
		case !aOK && bOK:
			chunks[k] = add{bVal}
		case aOK && !bOK:
			chunks[k] = del{seq(k)}
		case !aOK && !bOK:
			panic("unreachable!")
		}
	}
	return chunks
}

type seq string

func (a seq) Eq(b Eq) bool {
	return a == b.(seq)
}
