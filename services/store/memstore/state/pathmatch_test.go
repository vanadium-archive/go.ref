package state_test

import (
	"fmt"
	"testing"

	"veyron/services/store/memstore/state"
	"veyron2/security"
	"veyron2/storage"
)

// Simple DAG based on teams and players.
func TestPathMatchDAG(t *testing.T) {
	st := state.New(rootPublicID)
	sn := st.MutableSnapshot()
	johnID := mkdir(t, sn, rootPublicID, "/teamsapp/players/john")
	janeID := mkdir(t, sn, rootPublicID, "/teamsapp/players/jane")
	joanID := mkdir(t, sn, rootPublicID, "/teamsapp/players/joan")
	link(t, sn, rootPublicID, "/teamsapp/teams/rockets/players/john", johnID)
	link(t, sn, rootPublicID, "/teamsapp/teams/rockets/players/jane", janeID)
	link(t, sn, rootPublicID, "/teamsapp/teams/hornets/players/jane", janeID)
	link(t, sn, rootPublicID, "/teamsapp/teams/hornets/players/joan", joanID)

	rJohn, _ := state.CompilePathRegex(".../john")
	if !sn.PathMatch(rootPublicID, johnID, rJohn) {
		t.Errorf("Expected match")
	}

	rTRockets, _ := state.CompilePathRegex(".../teams/rockets/...")
	if !sn.PathMatch(rootPublicID, johnID, rTRockets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTRockets) {
		t.Errorf("Expected match")
	}
	if sn.PathMatch(rootPublicID, joanID, rTRockets) {
		t.Errorf("Unexpected match")
	}

	rTHornets, _ := state.CompilePathRegex(".../teams/hornets/...")
	if sn.PathMatch(rootPublicID, johnID, rTHornets) {
		t.Errorf("Unexpected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTHornets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, joanID, rTHornets) {
		t.Errorf("Expected match")
	}

	rTRocketsOrHornets, _ := state.CompilePathRegex(".../teams/{rockets,hornets}/...")
	if !sn.PathMatch(rootPublicID, johnID, rTRocketsOrHornets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTRocketsOrHornets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, joanID, rTRocketsOrHornets) {
		t.Errorf("Expected match")
	}

	rTJoanOrRockets, _ := state.CompilePathRegex(".../{players/joan,teams/rockets}/...")
	if !sn.PathMatch(rootPublicID, johnID, rTJoanOrRockets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTJoanOrRockets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, joanID, rTJoanOrRockets) {
		t.Errorf("Expected match")
	}
}

// Similar to above, but introduce loops by adding a teams directory to each of
// the players, looping back to their teams.
func TestPathMatchLoop(t *testing.T) {
	st := state.New(rootPublicID)
	sn := st.MutableSnapshot()
	johnID := mkdir(t, sn, rootPublicID, "/teamsapp/players/john")
	janeID := mkdir(t, sn, rootPublicID, "/teamsapp/players/jane")
	joanID := mkdir(t, sn, rootPublicID, "/teamsapp/players/joan")
	rocketsID := mkdir(t, sn, rootPublicID, "/teamsapp/teams/rockets")
	hornetsID := mkdir(t, sn, rootPublicID, "/teamsapp/teams/hornets")
	link(t, sn, rootPublicID, "/teamsapp/teams/rockets/players/john", johnID)
	link(t, sn, rootPublicID, "/teamsapp/teams/rockets/players/jane", janeID)
	link(t, sn, rootPublicID, "/teamsapp/teams/hornets/players/jane", janeID)
	link(t, sn, rootPublicID, "/teamsapp/teams/hornets/players/joan", joanID)
	link(t, sn, rootPublicID, "/teamsapp/players/john/teams/rockets", rocketsID)
	link(t, sn, rootPublicID, "/teamsapp/players/jane/teams/rockets", rocketsID)
	link(t, sn, rootPublicID, "/teamsapp/players/jane/teams/hornets", hornetsID)
	link(t, sn, rootPublicID, "/teamsapp/players/joan/teams/hornets", hornetsID)
	if err := st.ApplyMutations(sn.Mutations()); err != nil {
		t.Errorf("ApplyMutations failed: %s", err)
	}

	rJohn, _ := state.CompilePathRegex(".../john")
	if !sn.PathMatch(rootPublicID, johnID, rJohn) {
		t.Errorf("Expected match")
	}

	rTRockets, _ := state.CompilePathRegex(".../teams/rockets/players/*")
	if !sn.PathMatch(rootPublicID, johnID, rTRockets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTRockets) {
		t.Errorf("Expected match")
	}
	if sn.PathMatch(rootPublicID, joanID, rTRockets) {
		t.Errorf("Unexpected match")
	}

	rTHornets, _ := state.CompilePathRegex(".../teams/hornets/players/*")
	if sn.PathMatch(rootPublicID, johnID, rTHornets) {
		t.Errorf("Unexpected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTHornets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, joanID, rTHornets) {
		t.Errorf("Expected match")
	}

	rTFancyPath, _ := state.CompilePathRegex(".../teams/rockets/players/*/teams/hornets/players/*")
	if sn.PathMatch(rootPublicID, johnID, rTFancyPath) {
		t.Errorf("Unexpected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTFancyPath) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, joanID, rTFancyPath) {
		t.Errorf("Expected match")
	}
}

// Similar to above, but use the explicit E field rather than the implicit
// directory.
func TestPathMatchFieldDAG(t *testing.T) {
	st := state.New(rootPublicID)
	sn := st.MutableSnapshot()
	johnID := mkdir(t, sn, rootPublicID, "/E/teamsapp/E/players/E/john")
	janeID := mkdir(t, sn, rootPublicID, "/E/teamsapp/E/players/E/jane")
	joanID := mkdir(t, sn, rootPublicID, "/E/teamsapp/E/players/E/joan")
	link(t, sn, rootPublicID, "/E/teamsapp/E/teams/E/rockets/E/players/E/john", johnID)
	link(t, sn, rootPublicID, "/E/teamsapp/E/teams/E/rockets/E/players/E/jane", janeID)
	link(t, sn, rootPublicID, "/E/teamsapp/E/teams/E/hornets/E/players/E/jane", janeID)
	link(t, sn, rootPublicID, "/E/teamsapp/E/teams/E/hornets/E/players/E/joan", joanID)
	if err := st.ApplyMutations(sn.Mutations()); err != nil {
		t.Errorf("ApplyMutations failed: %s", err)
	}

	rJohn, _ := state.CompilePathRegex(".../E/john")
	if !sn.PathMatch(rootPublicID, johnID, rJohn) {
		t.Errorf("Expected match")
	}

	rTRockets, _ := state.CompilePathRegex(".../E/teams/E/rockets/E/...")
	if !sn.PathMatch(rootPublicID, johnID, rTRockets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTRockets) {
		t.Errorf("Expected match")
	}
	if sn.PathMatch(rootPublicID, joanID, rTRockets) {
		t.Errorf("Unexpected match")
	}

	rTHornets, _ := state.CompilePathRegex(".../E/teams/E/hornets/E/...")
	if sn.PathMatch(rootPublicID, johnID, rTHornets) {
		t.Errorf("Unexpected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTHornets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, joanID, rTHornets) {
		t.Errorf("Expected match")
	}

	rTRocketsOrHornets, _ := state.CompilePathRegex(".../E/teams/E/{rockets,hornets}/E/...")
	if !sn.PathMatch(rootPublicID, johnID, rTRocketsOrHornets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTRocketsOrHornets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, joanID, rTRocketsOrHornets) {
		t.Errorf("Expected match")
	}

	rTJoanOrRockets, _ := state.CompilePathRegex(".../E/{players/E/joan,teams/E/rockets}/...")
	if !sn.PathMatch(rootPublicID, johnID, rTJoanOrRockets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTJoanOrRockets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, joanID, rTJoanOrRockets) {
		t.Errorf("Expected match")
	}
}

func TestPathMatchFieldLoop(t *testing.T) {
	st := state.New(rootPublicID)
	sn := st.MutableSnapshot()
	johnID := mkdir(t, sn, rootPublicID, "/E/teamsapp/E/players/E/john")
	janeID := mkdir(t, sn, rootPublicID, "/E/teamsapp/E/players/E/jane")
	joanID := mkdir(t, sn, rootPublicID, "/E/teamsapp/E/players/E/joan")
	rocketsID := mkdir(t, sn, rootPublicID, "/E/teamsapp/E/teams/E/rockets")
	hornetsID := mkdir(t, sn, rootPublicID, "/E/teamsapp/E/teams/E/hornets")
	link(t, sn, rootPublicID, "/E/teamsapp/E/teams/E/rockets/E/players/E/john", johnID)
	link(t, sn, rootPublicID, "/E/teamsapp/E/teams/E/rockets/E/players/E/jane", janeID)
	link(t, sn, rootPublicID, "/E/teamsapp/E/teams/E/hornets/E/players/E/jane", janeID)
	link(t, sn, rootPublicID, "/E/teamsapp/E/teams/E/hornets/E/players/E/joan", joanID)
	link(t, sn, rootPublicID, "/E/teamsapp/E/players/E/john/E/teams/E/rockets", rocketsID)
	link(t, sn, rootPublicID, "/E/teamsapp/E/players/E/jane/E/teams/E/rockets", rocketsID)
	link(t, sn, rootPublicID, "/E/teamsapp/E/players/E/jane/E/teams/E/hornets", hornetsID)
	link(t, sn, rootPublicID, "/E/teamsapp/E/players/E/joan/E/teams/E/hornets", hornetsID)
	if err := st.ApplyMutations(sn.Mutations()); err != nil {
		t.Errorf("ApplyMutations failed: %s", err)
	}

	rJohn, _ := state.CompilePathRegex(".../E/john")
	if !sn.PathMatch(rootPublicID, johnID, rJohn) {
		t.Errorf("Expected match")
	}

	rTRockets, _ := state.CompilePathRegex(".../E/teams/E/rockets/E/players/E/*")
	if !sn.PathMatch(rootPublicID, johnID, rTRockets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTRockets) {
		t.Errorf("Expected match")
	}
	if sn.PathMatch(rootPublicID, joanID, rTRockets) {
		t.Errorf("Unexpected match")
	}

	rTHornets, _ := state.CompilePathRegex(".../E/teams/E/hornets/E/players/E/*")
	if sn.PathMatch(rootPublicID, johnID, rTHornets) {
		t.Errorf("Unexpected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTHornets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, joanID, rTHornets) {
		t.Errorf("Expected match")
	}

	rTFancyPath, _ := state.CompilePathRegex(".../E/teams/E/rockets/E/players/E/*/E/teams/E/hornets/E/players/E/*")
	if sn.PathMatch(rootPublicID, johnID, rTFancyPath) {
		t.Errorf("Unexpected match")
	}
	if !sn.PathMatch(rootPublicID, janeID, rTFancyPath) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(rootPublicID, joanID, rTFancyPath) {
		t.Errorf("Expected match")
	}
}

// Create a player, and add security restrictions.
func mkSecureHome(t *testing.T, sn *state.MutableSnapshot, pid security.PublicID, name string, user security.PrincipalPattern) (storage.ID, storage.TagList) {
	id := mkdir(t, sn, rootPublicID, fmt.Sprintf("/teamsapp/home/%s", name))
	aclID := putPath(t, sn, rootPublicID, fmt.Sprintf("/teamsapp/home/%s/acls/rwa", name), &storage.ACL{
		Name: name,
		Contents: security.ACL{
			user: security.LabelSet(security.ReadLabel | security.WriteLabel | security.AdminLabel),
		},
	})
	tags := storage.TagList{
		storage.Tag{Op: storage.RemoveACL, ACL: state.EveryoneACLID},
		storage.Tag{Op: storage.AddInheritedACL, ACL: aclID},
	}
	put(t, sn, rootPublicID, fmt.Sprintf("/teamsapp/home/%s/.tags", name), tags)
	return id, tags
}

// Check security restrictions.
func TestPathMatchSecurity(t *testing.T) {
	st := state.New(rootPublicID)
	sn := st.MutableSnapshot()

	// Create the players and set their ACLs.
	johnID, johnTags := mkSecureHome(t, sn, rootPublicID, "john", johnUser)
	janeID, janeTags := mkSecureHome(t, sn, rootPublicID, "jane", janeUser)
	joanID, _ := mkSecureHome(t, sn, rootPublicID, "joan", joanUser)
	rocketsID := mkdir(t, sn, rootPublicID, "/teamsapp/teams/rockets")
	hornetsID := mkdir(t, sn, rootPublicID, "/teamsapp/teams/hornets")
	link(t, sn, rootPublicID, "/teamsapp/teams/rockets/players/john", johnID)
	link(t, sn, rootPublicID, "/teamsapp/teams/rockets/players/jane", janeID)
	link(t, sn, rootPublicID, "/teamsapp/teams/hornets/players/jane", janeID)
	link(t, sn, rootPublicID, "/teamsapp/teams/hornets/players/joan", joanID)
	link(t, sn, rootPublicID, "/teamsapp/home/john/teams/rockets", rocketsID)
	link(t, sn, rootPublicID, "/teamsapp/home/jane/teams/rockets", rocketsID)
	link(t, sn, rootPublicID, "/teamsapp/home/jane/teams/hornets", hornetsID)
	link(t, sn, rootPublicID, "/teamsapp/home/joan/teams/hornets", hornetsID)
	if err := st.ApplyMutations(sn.Mutations()); err != nil {
		t.Errorf("ApplyMutations failed: %s", err)
	}

	sn = st.MutableSnapshot()
	rJohn, _ := state.CompilePathRegex(".../john")
	if !sn.PathMatch(rootPublicID, johnID, rJohn) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(johnPublicID, johnID, rJohn) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(janePublicID, johnID, rJohn) {
		t.Errorf("Expected match")
	}

	rTRockets, _ := state.CompilePathRegex(".../teams/rockets/players/*")
	if !sn.PathMatch(janePublicID, johnID, rTRockets) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(janePublicID, janeID, rTRockets) {
		t.Errorf("Expected match")
	}
	if sn.PathMatch(janePublicID, joanID, rTRockets) {
		t.Errorf("Unexpected match")
	}

	rHomeJane, _ := state.CompilePathRegex(".../home/jane")
	if _, err := sn.Get(johnPublicID, storage.ParsePath("/teamsapp/home/jane")); err == nil {
		t.Errorf("Security error")
	}
	// John can't see Jane's home path.
	if sn.PathMatch(johnPublicID, janeID, rHomeJane) {
		t.Errorf("Unexpected match")
	}
	// Jane can see it.
	if !sn.PathMatch(janePublicID, janeID, rHomeJane) {
		t.Errorf("Expected match")
	}

	// Both can see Jane through the teams directory.
	rPlayersJane, _ := state.CompilePathRegex(".../players/jane")
	if !sn.PathMatch(johnPublicID, janeID, rPlayersJane) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(janePublicID, janeID, rPlayersJane) {
		t.Errorf("Expected match")
	}

	// Restrict /teamsapp/teams to Jane.
	put(t, sn, rootPublicID, "/teamsapp/teams/.tags", janeTags)
	// John can still see through /teamsapp/home/john/teams/rockets/players/jane.
	if !sn.PathMatch(johnPublicID, janeID, rPlayersJane) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(janePublicID, janeID, rPlayersJane) {
		t.Errorf("Expected match")
	}

	// Restrict /teamsapp/teams/rockets.
	put(t, sn, rootPublicID, "/teamsapp/teams/rockets/.tags", janeTags)
	// We took away EveryoneACLID, but John is still inherited.
	if !sn.PathMatch(johnPublicID, janeID, rPlayersJane) {
		t.Errorf("Expected match")
	}
	if !sn.PathMatch(janePublicID, janeID, rPlayersJane) {
		t.Errorf("Expected match")
	}

	// Take away John from the rockets too.
	tag := storage.Tag{Op: storage.RemoveACL, ACL: johnTags[1].ACL}
	put(t, sn, rootPublicID, "/teamsapp/teams/rockets/.tags/@", tag)
	// John is now locked out.
	if sn.PathMatch(johnPublicID, janeID, rPlayersJane) {
		t.Errorf("Unexpected match")
	}
	if !sn.PathMatch(janePublicID, janeID, rPlayersJane) {
		t.Errorf("Expected match")
	}
}
