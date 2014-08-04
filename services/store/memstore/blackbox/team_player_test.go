package blackbox

import (
	"runtime"
	"testing"

	"veyron/services/store/memstore"
	"veyron/services/store/memstore/state"

	"veyron2/storage"
	"veyron2/vom"
)

func init() {
	vom.Register(&Player{})
	vom.Register(&Team{})
}

// This schema uses the Team/Player schema in the query doc.
//
//     Player : belongs to many teams.
//     Team : contains many players.

// Player is a person who has a Role.
type Player struct {
	FullName string
}

// Team has a set of Roles/
type Team struct {
	FullName string
	Players  []storage.ID
}

func newPlayer(name string) *Player {
	return &Player{FullName: name}
}

func newTeam(name string) *Team {
	return &Team{FullName: name}
}

func getPlayer(t *testing.T, st *memstore.Store, tr *memstore.Transaction, path string) (storage.ID, *Player) {
	_, file, line, _ := runtime.Caller(1)
	e := Get(t, st, tr, path)
	p, ok := e.Value.(*Player)
	if !ok {
		t.Fatalf("%s(%d): %s: not a Player: %v", file, line, path, e.Value)
	}
	return e.Stat.ID, p
}

func getTeam(t *testing.T, st *memstore.Store, tr *memstore.Transaction, path string) (storage.ID, *Team) {
	_, file, line, _ := runtime.Caller(1)
	e := Get(t, st, tr, path)
	p, ok := e.Value.(*Team)
	if !ok {
		t.Fatalf("%s(%d): %s: not a Team: %v", file, line, path, e.Value)
	}
	return e.Stat.ID, p
}

func TestManyToManyWithRole(t *testing.T) {
	st, err := memstore.New(rootPublicID, "")
	if err != nil {
		t.Fatalf("memstore.New() failed: %v", err)
	}

	john := newPlayer("John")
	jane := newPlayer("Jane")
	joan := newPlayer("Joan")

	rockets := newTeam("Rockets")
	hornets := newTeam("Hornets")

	// Create the state.
	{
		tr := memstore.NewTransaction()
		Put(t, st, tr, "/", newDir())
		Put(t, st, tr, "/teamsapp", newDir())
		Put(t, st, tr, "/teamsapp/teams", newDir())
		Put(t, st, tr, "/teamsapp/teams/rockets", rockets)
		Put(t, st, tr, "/teamsapp/teams/hornets", hornets)
		Put(t, st, tr, "/teamsapp/players", newDir())

		johnID := Put(t, st, tr, "/teamsapp/teams/rockets/Players/@", john)
		janeID := Put(t, st, tr, "/teamsapp/teams/rockets/Players/@", jane)
		Put(t, st, tr, "/teamsapp/teams/hornets/Players/@", janeID)
		joanID := Put(t, st, tr, "/teamsapp/teams/hornets/Players/@", joan)

		Put(t, st, tr, "/teamsapp/players/John", johnID)
		Put(t, st, tr, "/teamsapp/players/Jane", janeID)
		Put(t, st, tr, "/teamsapp/players/Joan", joanID)

		Commit(t, tr)
	}

	rocketsID := Get(t, st, nil, "/teamsapp/teams/rockets").Stat.ID
	hornetsID := Get(t, st, nil, "/teamsapp/teams/hornets").Stat.ID
	johnID := Get(t, st, nil, "/teamsapp/players/John").Stat.ID
	janeID := Get(t, st, nil, "/teamsapp/players/Jane").Stat.ID
	joanID := Get(t, st, nil, "/teamsapp/players/Joan").Stat.ID

	// Verify some of the state.
	{
		tr := memstore.NewTransaction()
		_, john := getPlayer(t, st, tr, "/teamsapp/players/John")
		_, rockets := getTeam(t, st, tr, "/teamsapp/teams/rockets")

		if john.FullName != "John" {
			t.Errorf("Expected %q, got %q", "John", john.FullName)
		}
		if len(rockets.Players) != 2 {
			t.Fatalf("Expected two players: got %v", rockets.Players)
		}
		if rockets.Players[0] != johnID {
			t.Errorf("Expected %s, got %s", johnID, rockets.Players[0])
		}
	}

	// Iterate over the rockets.
	players := make(map[storage.ID]*Player)
	name := storage.ParsePath("/teamsapp/players")
	for it := st.Snapshot().NewIterator(rootPublicID, name,
		state.ListPaths, nil); it.IsValid(); it.Next() {

		e := it.Get()
		if p, ok := e.Value.(*Player); ok {
			if _, ok := players[e.Stat.ID]; ok {
				t.Errorf("Player already exists: %v", p)
				continue
			}
			players[e.Stat.ID] = p
		}
	}
	if len(players) != 3 {
		t.Errorf("Should have 3 players: have %v", players)
	}
	if p, ok := players[johnID]; !ok || p.FullName != "John" {
		t.Errorf("Should have John, have %v", p)
	}
	if p, ok := players[janeID]; !ok || p.FullName != "Jane" {
		t.Errorf("Should have Jane, have %v", p)
	}
	if p, ok := players[joanID]; !ok || p.FullName != "Joan" {
		t.Errorf("Should have Joan, have %v", p)
	}

	// Iterate over all teams, nonrecursively.
	teams := make(map[storage.ID]*Team)
	name = storage.ParsePath("/teamsapp/teams")
	for it := st.Snapshot().NewIterator(rootPublicID, name,
		state.ListPaths, state.ImmediateFilter); it.IsValid(); it.Next() {

		e := it.Get()
		v := e.Value
		if _, ok := v.(*Player); ok {
			t.Errorf("Nonrecursive iteration should not show players")
		}
		if team, ok := v.(*Team); ok {
			if _, ok := teams[e.Stat.ID]; ok {
				t.Errorf("Team already exists: %v", team)
				continue
			}
			teams[e.Stat.ID] = team
		}
	}
	if len(teams) != 2 {
		t.Errorf("Should have 2 teams: have %v", teams)
	}
	if team, ok := teams[rocketsID]; !ok || team.FullName != "Rockets" {
		t.Errorf("Should have Rockets, have %v", team)
	}
	if team, ok := teams[hornetsID]; !ok || team.FullName != "Hornets" {
		t.Errorf("Should have Hornets, have %v", team)
	}

	// Iterate over all teams, recursively.
	contractCount := 0
	teamCount := 0
	players = make(map[storage.ID]*Player)
	teams = make(map[storage.ID]*Team)
	name = storage.ParsePath("/teamsapp/teams")
	for it := st.Snapshot().NewIterator(rootPublicID, name,
		state.ListPaths, nil); it.IsValid(); it.Next() {

		e := it.Get()
		v := e.Value
		if p, ok := v.(*Player); ok {
			players[e.Stat.ID] = p
			contractCount++
		}
		if team, ok := v.(*Team); ok {
			teams[e.Stat.ID] = team
			teamCount++
		}
	}
	if teamCount != 2 {
		t.Errorf("Should have 2 teams: have %d", teamCount)
	}
	if len(teams) != 2 {
		t.Errorf("Should have 2 teams: have %v", teams)
	}
	if team, ok := teams[rocketsID]; !ok || team.FullName != "Rockets" {
		t.Errorf("Should have Rockets, have %v", team)
	}
	if team, ok := teams[hornetsID]; !ok || team.FullName != "Hornets" {
		t.Errorf("Should have Hornets, have %v", team)
	}
	if contractCount != 4 {
		t.Errorf("Should have 4 contracts: have %d", contractCount)
	}
	if len(players) != 3 {
		t.Errorf("Should have 3 players: have %v", players)
	}
	if p, ok := players[johnID]; !ok || p.FullName != "John" {
		t.Errorf("Should have John, have %v", p)
	}
	if p, ok := players[janeID]; !ok || p.FullName != "Jane" {
		t.Errorf("Should have Jane, have %v", p)
	}
	if p, ok := players[joanID]; !ok || p.FullName != "Joan" {
		t.Errorf("Should have Joan, have %v", p)
	}
}
