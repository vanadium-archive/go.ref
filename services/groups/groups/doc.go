// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
Command groups creates and manages Vanadium groups of blessing patterns.

Usage:
   groups <command>

The groups commands are:
   create      Creates a blessing pattern group
   delete      Delete a blessing group
   add         Adds a blessing pattern to a group
   remove      Removes a blessing pattern from a group
   relate      Relate a set of blessing to a group
   get         Returns entries of a group
   help        Display help for commands or topics

The global flags are:
 -v23.metadata=<just specify -v23.metadata to activate>
   Displays metadata for the program and exits.

Groups create - Creates a blessing pattern group

Creates a blessing pattern group.

Usage:
   groups create [flags] <von> <patterns...>

<von> is the vanadium object name of the group to create

<patterns...> is a list of blessing pattern chunks

The groups create flags are:
 -permissions=
   Path to a permissions file

Groups delete - Delete a blessing group

Delete a blessing group.

Usage:
   groups delete [flags] <von>

<von> is the vanadium object name of the group

The groups delete flags are:
 -version=
   Identifies group version

Groups add - Adds a blessing pattern to a group

Adds a blessing pattern to a group.

Usage:
   groups add [flags] <von> <pattern>

<von> is the vanadium object name of the group

<pattern> is the blessing pattern chunk to add

The groups add flags are:
 -version=
   Identifies group version

Groups remove - Removes a blessing pattern from a group

Removes a blessing pattern from a group.

Usage:
   groups remove [flags] <von> <pattern>

<von> is the vanadium object name of the group

<pattern> is the blessing pattern chunk to add

The groups remove flags are:
 -version=
   Identifies group version

Groups relate - Relate a set of blessing to a group

Relate a set of blessing to a group.

NOTE: This command exists primarily for debugging purposes. In particular,
invocations of the Relate RPC are expected to be mainly issued by the
authorization logic.

Usage:
   groups relate [flags] <von> <blessings>

<von> is the vanadium object name of the group

<blessings...> is a list of blessings

The groups relate flags are:
 -approximation=under
   Identifies the type of approximation to use; supported values = (under, over)
 -version=
   Identifies group version

Groups get - Returns entries of a group

Returns entries of a group.

Usage:
   groups get <von>

<von> is the vanadium object name of the group

Groups help - Display help for commands or topics

Help with no args displays the usage of the parent command.

Help with args displays the usage of the specified sub-command or help topic.

"help ..." recursively displays help for all commands and topics.

Usage:
   groups help [flags] [command/topic ...]

[command/topic ...] optionally identifies a specific sub-command or help topic.

The groups help flags are:
 -style=compact
   The formatting style for help output:
      compact - Good for compact cmdline output.
      full    - Good for cmdline output, shows all global flags.
      godoc   - Good for godoc processing.
   Override the default by setting the CMDLINE_STYLE environment variable.
 -width=<terminal width>
   Format output to this target width in runes, or unlimited if width < 0.
   Defaults to the terminal width if available.  Override the default by setting
   the CMDLINE_WIDTH environment variable.
*/
package main
