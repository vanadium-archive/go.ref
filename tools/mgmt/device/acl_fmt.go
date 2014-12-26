package main

import (
	"fmt"
	"sort"
	"strings"

	"v.io/core/veyron2/security"
)

// aclEntries maps blessing patterns to the kind of access they should have.
type aclEntries map[string]accessTags

// accessTags maps access tags to whether they should be blacklisted
// (i.e., part of the NotIn list) or not (part of the In list).
//
// TODO(ashankar,caprita): This structure is not friendly to a blessing
// appearing in both the "In" and "NotIn" lists of an ACL. Arguably, such an
// ACL is silly (In: ["foo"], NotIn: ["foo"]), but is legal. This structure can
// end up hiding that.
type accessTags map[string]bool

// String representation of access tags.  Between String and parseAccessTags,
// the "get" and "set" commands are able to speak the same language: the output
// of "get" and be copied/pasted into "set".
func (tags accessTags) String() string {
	// Sort tags and then apply "!".
	var list []string
	for tag, _ := range tags {
		list = append(list, tag)
	}
	sort.Strings(list)
	for ix, tag := range list {
		if tags[tag] {
			list[ix] = "!" + list[ix]
		}
	}
	return strings.Join(list, ",")
}

func parseAccessTags(input string) (accessTags, error) {
	ret := make(accessTags)
	if input == "^" {
		return ret, nil
	}
	for _, tag := range strings.Split(input, ",") {
		blacklist := strings.HasPrefix(tag, "!")
		if blacklist {
			tag = tag[1:]
		}
		if len(tag) == 0 {
			return nil, fmt.Errorf("empty access tag in %q", input)
		}
		ret[tag] = blacklist
	}
	return ret, nil
}

func (entries aclEntries) String() string {
	var list []string
	for pattern, _ := range entries {
		list = append(list, pattern)
	}
	sort.Strings(list)
	for ix, pattern := range list {
		list[ix] = fmt.Sprintf("%s %v", pattern, entries[pattern])
	}
	return strings.Join(list, "\n")
}

func (entries aclEntries) Tags(pattern string) accessTags {
	tags, exists := entries[pattern]
	if !exists {
		tags = make(accessTags)
		entries[pattern] = tags
	}
	return tags
}

type byPattern []security.BlessingPattern

func (a byPattern) Len() int           { return len(a) }
func (a byPattern) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byPattern) Less(i, j int) bool { return a[i] < a[j] }
