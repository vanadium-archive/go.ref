// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Syncbase client shell. Currently supports syncQL select queries.

package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	isatty "github.com/mattn/go-isatty"

	"v.io/v23/context"
	"v.io/v23/syncbase"
	"v.io/v23/syncbase/nosql"
	"v.io/x/lib/cmdline"
	"v.io/x/ref/cmd/sb/internal/demodb"
	"v.io/x/ref/cmd/sb/internal/reader"
	"v.io/x/ref/cmd/sb/internal/writer"
	"v.io/x/ref/lib/v23cmd"
)

var cmdSbShell = &cmdline.Command{
	Runner: v23cmd.RunnerFunc(runSbShell),
	Name:   "sh",
	Short:  "Start a syncQL shell",
	Long: `
Connect to a database on the Syncbase service and start a syncQL shell.
`,
	ArgsName: "<app_name> <db_name>",
	ArgsLong: `
<app_name> and <db_name> specify the database to execute queries against.
The database must exist unless -create-missing is specified.
`,
}

var (
	flagFormat            string
	flagCSVDelimiter      string
	flagCreateIfNotExists bool
	flagMakeDemoTables    bool
)

func init() {
	cmdSbShell.Flags.StringVar(&flagFormat, "format", "table", "Output format. 'table': human-readable table; 'csv': comma-separated values, use -csv-delimiter to control the delimiter; 'json': JSON objects.")
	cmdSbShell.Flags.StringVar(&flagCSVDelimiter, "csv-delimiter", ",", "Delimiter to use when printing data as CSV (e.g. \"\t\", \",\")")
	cmdSbShell.Flags.BoolVar(&flagCreateIfNotExists, "create-missing", false, "Create the app and/or database if they do not exist yet.")
	cmdSbShell.Flags.BoolVar(&flagMakeDemoTables, "make-demo", false, "(Re)create demo tables in the database.")
}

func validateFlags() error {
	if flagFormat != "table" && flagFormat != "csv" && flagFormat != "json" {
		return fmt.Errorf("Unsupported -format %q.  Must be one of 'table', 'csv', or 'json'.", flagFormat)
	}
	if len(flagCSVDelimiter) == 0 {
		return fmt.Errorf("-csv-delimiter cannot be empty.")
	}
	return nil
}

// Starts a syncQL shell against the specified database.
// Runs in interactive or batch mode depending on stdin.
func runSbShell(ctx *context.T, env *cmdline.Env, args []string) error {
	// TODO(ivanpi): Add 'use' statement, default to no app/database selected.
	if len(args) != 2 {
		return env.UsageErrorf("exactly two arguments expected")
	}
	appName, dbName := args[0], args[1]
	if err := validateFlags(); err != nil {
		return env.UsageErrorf("%v", err)
	}

	sbs := syncbase.NewService(*flagSbService)
	d, err := openAppDB(ctx, sbs, appName, dbName, flagCreateIfNotExists)
	if err != nil {
		return err
	}

	if flagMakeDemoTables {
		if err := makeDemoDB(ctx, env.Stdout, d); err != nil {
			return err
		}
	}

	var input *reader.T
	// TODO(ivanpi): This is hacky, it would be better for lib/cmdline to support IsTerminal.
	stdinFile, ok := env.Stdin.(*os.File)
	isTerminal := ok && isatty.IsTerminal(stdinFile.Fd())
	if isTerminal {
		input = reader.NewInteractive()
	} else {
		input = reader.NewNonInteractive()
	}
	defer input.Close()

stmtLoop:
	for true {
		if q, err := input.GetQuery(); err != nil {
			if err == io.EOF {
				if isTerminal {
					// ctrl-d
					fmt.Println()
				}
				break
			} else {
				// ctrl-c
				break
			}
		} else {
			var err error
			tq := strings.Fields(q)
			if len(tq) > 0 {
				switch strings.ToLower(tq[0]) {
				case "exit", "quit":
					break stmtLoop
				case "dump":
					err = dumpDB(ctx, env.Stdout, d)
				case "make-demo":
					// TODO(jkline): add an "Are you sure prompt" to give the user a 2nd chance.
					err = makeDemoDB(ctx, env.Stdout, d)
				case "select":
					err = queryExec(ctx, env.Stdout, d, q)
				default:
					err = fmt.Errorf("unknown statement: '%s'; expected one of: 'select', 'make-demo', 'dump', 'exit', 'quit'", strings.ToLower(tq[0]))
				}
			}
			if err != nil {
				if isTerminal {
					fmt.Fprintln(env.Stderr, "Error:", err)
				} else {
					// If running non-interactively, errors stop execution.
					return err
				}
			}
		}
	}

	return nil
}

func openAppDB(ctx *context.T, sbs syncbase.Service, appName, dbName string, createIfNotExists bool) (nosql.Database, error) {
	app := sbs.App(appName)
	if exists, err := app.Exists(ctx); err != nil {
		return nil, fmt.Errorf("failed checking for app %q: %v", app.FullName(), err)
	} else if !exists {
		if !createIfNotExists {
			return nil, fmt.Errorf("app %q does not exist", app.FullName())
		}
		if err := app.Create(ctx, nil); err != nil {
			return nil, err
		}
	}
	d := app.NoSQLDatabase(dbName, nil)
	if exists, err := d.Exists(ctx); err != nil {
		return nil, fmt.Errorf("failed checking for db %q: %v", d.FullName(), err)
	} else if !exists {
		if !createIfNotExists {
			return nil, fmt.Errorf("db %q does not exist", d.FullName())
		}
		if err := d.Create(ctx, nil); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func mergeErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	err := errs[0]
	for _, e := range errs[1:] {
		err = fmt.Errorf("%v\n%v", err, e)
	}
	return err
}

func dumpTables(ctx *context.T, w io.Writer, d nosql.Database) error {
	tables, err := d.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("failed listing tables: %v", err)
	}
	var errs []error
	for _, table := range tables {
		fmt.Fprintf(w, "table: %s\n", table)
		if err := queryExec(ctx, w, d, fmt.Sprintf("select k, v from %s", table)); err != nil {
			errs = append(errs, fmt.Errorf("> %s: %v", table, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed dumping %d of %d tables:\n%v", len(errs), len(tables), mergeErrors(errs))
	}
	return nil
}

func dumpSyncgroups(ctx *context.T, w io.Writer, d nosql.Database) error {
	sgNames, err := d.GetSyncgroupNames(ctx)
	if err != nil {
		return fmt.Errorf("failed listing syncgroups: %v", err)
	}
	var errs []error
	for _, sgName := range sgNames {
		fmt.Fprintf(w, "syncgroup: %s\n", sgName)
		sg := d.Syncgroup(sgName)
		if spec, version, err := sg.GetSpec(ctx); err != nil {
			errs = append(errs, err)
		} else {
			fmt.Fprintf(w, "%+v (version: \"%s\")\n", spec, version)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed dumping %d of %d syncgroups:\n%v", len(errs), len(sgNames), mergeErrors(errs))
	}
	return nil
}

func dumpDB(ctx *context.T, w io.Writer, d nosql.Database) error {
	var errors []error
	if err := dumpTables(ctx, w, d); err != nil {
		errors = append(errors, fmt.Errorf("failed dumping tables: %v", err))
	}
	if err := dumpSyncgroups(ctx, w, d); err != nil {
		errors = append(errors, fmt.Errorf("failed dumping syncgroups: %v", err))
	}
	return mergeErrors(errors)
}

func makeDemoDB(ctx *context.T, w io.Writer, d nosql.Database) error {
	if err := demodb.PopulateDemoDB(ctx, d); err == nil {
		fmt.Fprintln(w, "Demo tables created and populated.")
	} else {
		return fmt.Errorf("failed making demo tables: %v", err)
	}
	return nil
}

// Split an error message into an offset and the remaining (i.e., rhs of offset) message.
// The convention for syncql is "<module><optional-rpc>[offset]<remaining-message>".
func splitError(err error) (int64, string) {
	errMsg := err.Error()
	idx1 := strings.Index(errMsg, "[")
	idx2 := strings.Index(errMsg, "]")
	if idx1 == -1 || idx2 == -1 {
		return 0, errMsg
	}
	offsetString := errMsg[idx1+1 : idx2]
	offset, err := strconv.ParseInt(offsetString, 10, 64)
	if err != nil {
		return 0, errMsg
	}
	return offset, errMsg[idx2+1:]
}

func queryExec(ctx *context.T, w io.Writer, d nosql.Database, q string) error {
	if columnNames, rs, err := d.Exec(ctx, q); err != nil {
		off, msg := splitError(err)
		return fmt.Errorf("\n%s\n%s^\n%d: %s", q, strings.Repeat(" ", int(off)), off+1, msg)
	} else {
		switch flagFormat {
		case "table":
			if err := writer.WriteTable(w, columnNames, rs); err != nil {
				return err
			}
		case "csv":
			if err := writer.WriteCSV(w, columnNames, rs, flagCSVDelimiter); err != nil {
				return err
			}
		case "json":
			if err := writer.WriteJson(w, columnNames, rs); err != nil {
				return err
			}
		default:
			panic(fmt.Sprintf("invalid format flag value: %v", flagFormat))
		}
	}
	return nil
}
