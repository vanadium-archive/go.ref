// Exercises basic store operations under various conditions.
//
// NOTE(sadovsky):
//  - It would be nice if Dir were a builtin (provided by store).
//  - It would be nice to have "mkdir -p" functionality for "put" commands.
//  - It's not clear how to decide between map[string]store.ID and the implicit
//    subdir mechanism.
//  - It'd be nice to not have to specify names for list items in the case where
//    we use the implicit subdir mechanism (rather than a slice).
//  - Why does "vgo test" take so long to compile? I thought Go compilation was
//    supposed to be fast...

package test

import (
	"fmt"
	"log"
	"runtime"
	"testing"

	bb "veyron/lib/testutil/blackbox"

	"veyron2/naming"
	"veyron2/rt"
	"veyron2/storage"
	"veyron2/vom"
)

func init() {
	bb.CommandTable["startServerRemote"] = startServerRemote
}

func vomRegister() {
	vom.Register(&List{})
	vom.Register(&Todo{})
}

func TestHelperProcess(t *testing.T) {
	bb.HelperProcess(t)
}

func startServerRemote(argv []string) {
	getRuntime() // initialize Runtime

	if len(argv) > 1 || (len(argv) == 1 && argv[0] != "vomRegister") {
		log.Fatal("Failed to start remote server: ", argv)
	} else if len(argv) == 1 {
		vomRegister()
	}
	name, cl := startServer()
	fmt.Println("ready")
	fmt.Println(name)

	bb.WaitForEOFOnStdin()
	fmt.Println("done")
	cl()
}

////////////////////////////////////////////////////////////////////////////////
// Structs and helpers

// Dir is a "directory" in the store.
type Dir struct{}

// List is a list of Todo items.
type List struct {
	Dir
	// TODO(sadovsky): Should we hold a slice (or map) of Todo names here, or
	// simply prefix Todo names with List names, e.g.
	// /lists/[list_name]/[todo_name]? For now, we do the former, since the query
	// and name server listdir APIs are not yet implemented.
	Todos []storage.ID
}

// Todo is a single task to be done.
type Todo struct {
	Dir
	Text string
	Done bool
	Tags []string
}

func newDir() *Dir {
	return &Dir{}
}

func newList() *List {
	return &List{Dir: *newDir()}
}

func newTodo(text string) *Todo {
	return &Todo{Dir: *newDir(), Text: text}
}

////////////////////////////////////////////////////////////////////////////////
// Type-specific helpers

func getList(t *testing.T, st storage.Store, path string) *List {
	_, file, line, _ := runtime.Caller(1)
	v := get(t, st, path)
	res, ok := v.(*List)
	if !ok {
		t.Fatalf("%s(%d): %s: not a List: %v", file, line, path, v)
	}
	return res
}

func getTodo(t *testing.T, st storage.Store, path string) *Todo {
	_, file, line, _ := runtime.Caller(1)
	v := get(t, st, path)
	res, ok := v.(*Todo)
	if !ok {
		t.Fatalf("%s(%d): %s: not a Todo: %v", file, line, path, v)
	}
	return res
}

////////////////////////////////////////////////////////////////////////////////
// Test cases

func testTodos(t *testing.T, st storage.Store) {
	ctx := rt.R().NewContext()

	// Create lists.
	{
		// NOTE(sadovsky): Currently, we can't put /x/y until we put / and /x.
		tname := createTransaction(t, st, "")
		put(t, st, tname, newDir())
		put(t, st, naming.Join(tname, "lists"), newDir())
		put(t, st, naming.Join(tname, "lists/drinks"), newList())
		put(t, st, naming.Join(tname, "lists/snacks"), newList())
		commit(t, st, tname)
	}

	// Add some todos.
	{
		tname := createTransaction(t, st, "")
		// NOTE(sadovsky): It feels awkward to create my own names (ids) for these
		// Todo objects. I'd like some way to create them in some "directory"
		// without explicitly naming them. I.e. in this case I want to think of the
		// directory as a list, not a map.
		put(t, st, naming.Join(tname, "lists/drinks/Todos/@"), newTodo("milk"))
		put(t, st, naming.Join(tname, "lists/drinks/Todos/@"), newTodo("beer"))
		put(t, st, naming.Join(tname, "lists/snacks/Todos/@"), newTodo("chips"))
		commit(t, st, tname)
	}

	// Verify some of the photos.
	{
		todo := getTodo(t, st, "/lists/drinks/Todos/0")
		if todo.Text != "milk" {
			t.Errorf("Expected %q, got %q", "milk", todo.Text)
		}
	}

	{
		todo := getTodo(t, st, "/lists/snacks/Todos/0")
		if todo.Text != "chips" {
			t.Errorf("Expected %q, got %q", "chips", todo.Text)
		}
	}

	// Move a todo item from one list to another.
	{
		tname := createTransaction(t, st, "")
		todo := getTodo(t, st, naming.Join(tname, "lists/drinks/Todos/1"))
		// NOTE(sadovsky): Remove works for map entries, but not yet for slices.
		// Instead, we read the list, prune it, and write it back.
		//remove(t, st, tr, "/lists/drinks/Todos/1")
		list := getList(t, st, naming.Join(tname, "lists/drinks"))
		list.Todos = list.Todos[:1]
		put(t, st, naming.Join(tname, "lists/drinks"), list)
		put(t, st, naming.Join(tname, "lists/snacks/Todos/@"), todo)
		commit(t, st, tname)
	}

	// Verify that the original todo is no longer there.
	// TODO(sadovsky): Use queries to verify that both lists have changed.
	{
		path := "/lists/drinks/1"
		if _, err := st.BindObject(path).Get(ctx); err == nil {
			t.Fatalf("Got removed object: %s", path)
		}
	}
}

func TestTodosWithLocalServer(t *testing.T) {
	// Initialize Runtime and register vom types (for both client and server).
	getRuntime()
	vomRegister()

	st, cl := startServerAndMakeClient()
	defer cl()
	testTodos(t, st)
}

func testTodosWithRemoteServer(t *testing.T, doVomRegister bool) {
	// Initialize Runtime and register vom types (for client but not server).
	getRuntime()
	vomRegister()

	args := []string{}
	if doVomRegister {
		args = append(args, "vomRegister")
	}
	server := bb.HelperCommand(t, "startServerRemote", args...)
	defer server.Cleanup()
	server.Cmd.Start()
	server.Expect("ready") // wait for server to be ready

	oa, err := server.ReadLineFromChild()
	if err != nil {
		t.Fatal("Failed to read server OA: %v", err)
	}

	st, cl := makeClient(oa)
	defer cl()
	testTodos(t, st)

	server.CloseStdin()
	server.Expect("done")
	server.ExpectEOFAndWait()
}

func TestTodosWithRemoteServer(t *testing.T) {
	testTodosWithRemoteServer(t, true)
}

// TODO(sadovsky): This test fails with the following error because the vom
// types aren't registered with the store server. We need to fix the store to
// support unregistered types.
// store_test.go(146): can't put /: ipc: response decoding failed: EOF
func DisabledTestTodosWithRemoteServerNoRegister(t *testing.T) {
	testTodosWithRemoteServer(t, false)
}
