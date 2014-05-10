package state_test

import (
	"reflect"
	"testing"

	"veyron/services/store/memstore/state"

	"veyron2/security"
	"veyron2/storage"
)

func TestSecurity(t *testing.T) {
	st := state.New(rootPublicID)
	sn := st.MutableSnapshot()

	// Create /Users/jane and give her RWA permissions.
	aclID := putPath(t, sn, rootPublicID, "/Users/jane/acls/janeRWA", &storage.ACL{
		Name: "Jane",
		Contents: security.ACL{
			janeUser: security.LabelSet(security.ReadLabel | security.WriteLabel | security.AdminLabel),
		},
	})
	janeTags := storage.TagList{storage.Tag{Op: storage.AddInheritedACL, ACL: aclID}}
	put(t, sn, rootPublicID, "/Users/jane/.tags", janeTags)

	// John should not be able to do anything.
	{
		if _, err := maybePut(sn, johnPublicID, "/", Node{}); err == nil {
			t.Errorf("Security violation")
		}
		if _, err := maybePut(sn, johnPublicID, "/Users/jane", Node{}); err == nil {
			t.Errorf("Security violation")
		}
		if _, err := maybeGet(sn, johnPublicID, "/Users/jane/.tags"); err == nil {
			t.Errorf("Security violation")
		}
	}

	// Jane can access her own directory.
	{
		if _, err := maybePut(sn, janePublicID, "/", Node{}); err == nil {
			t.Errorf("Security violation")
		}
		if _, err := maybeGet(sn, janePublicID, "/Users/jane"); err != nil {
			t.Errorf("Expected /Users/jane to exist: %s", err)
		}
		if _, err := maybePut(sn, janePublicID, "/Users/jane", Node{}); err != nil {
			t.Errorf("Unexpected security error: %s %s", err, janePublicID)
		}
		if tags, err := maybeGet(sn, janePublicID, "/Users/jane/.tags"); err == nil {
			if !reflect.DeepEqual(janeTags, tags) {
				t.Errorf("Expected %+v, got %+v", janeTags, tags)
			}
		} else {
			t.Errorf("Unexpected security error: %s", err)
		}
	}

	// Jane gives John read/write permission.
	var johnTag storage.Tag
	{
		aclID := putPath(t, sn, janePublicID, "/Users/jane/acls/johnRW", storage.ACL{
			Name: "John",
			Contents: security.ACL{
				johnUser: security.LabelSet(security.ReadLabel | security.WriteLabel),
			},
		})
		johnTag = storage.Tag{Op: storage.AddInheritedACL, ACL: aclID}
		// The @ is a pseudo-index, meaning append the tag to the list of tags.
		put(t, sn, janePublicID, "/Users/jane/.tags/@", johnTag)
	}

	// Jane can still access.
	janeTags = append(janeTags, johnTag)
	{
		if _, err := maybeGet(sn, janePublicID, "/Users/jane"); err != nil {
			t.Errorf("Expected /Users/jane to exist: %s", err)
		}
		if _, err := maybePut(sn, janePublicID, "/Users/jane", Node{}); err != nil {
			t.Errorf("Unexpected security error: %s", err)
		}
		if tags, err := maybeGet(sn, janePublicID, "/Users/jane/.tags"); err == nil {
			if !reflect.DeepEqual(janeTags, tags) {
				t.Errorf("Expected %+v, got %+v", janeTags, tags)
			}
		} else {
			t.Errorf("Unexpected security error: %s", err)
		}
	}

	// John also has access.
	{
		if _, err := maybePut(sn, johnPublicID, "/Users/jane", Node{}); err != nil {
			t.Errorf("Unexpected error: %s", err)
		}
		mkdir(t, sn, johnPublicID, "/Users/jane/john")

		// John is still not allowed to access the tags.
		if _, err := maybeGet(sn, johnPublicID, "/Users/jane/.tags"); err == nil {
			t.Errorf("Security violation")
		}
	}
}
