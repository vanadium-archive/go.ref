// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
The uniqueid tool generates unique ids. It also has an option of automatically
substituting unique ids with placeholders in files.

Usage:
   uniqueid <command>

The uniqueid commands are:
   generate    Generates UniqueIds
   inject      Injects UniqueIds into existing files
   help        Display help for commands or topics
Run "uniqueid help [command]" for command usage.

Uniqueid Generate

Generates unique ids and outputs them to standard out.

Usage:
   uniqueid generate

Uniqueid Inject

Injects UniqueIds into existing files. Strings of the form "$UNIQUEID$" will be
replaced with generated ids.

Usage:
   uniqueid inject <filenames>

<filenames> List of files to inject unique ids into

Uniqueid Help

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

The output is formatted to a target width in runes.  The target width is
determined by checking the environment variable CMDLINE_WIDTH, falling back on
the terminal width from the OS, falling back on 80 chars.  By setting
CMDLINE_WIDTH=x, if x > 0 the width is x, if x < 0 the width is unlimited, and
if x == 0 or is unset one of the fallbacks is used.

Usage:
   uniqueid help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The uniqueid help flags are:
 -style=default
   The formatting style for help output, either "default" or "godoc".
*/
package main
