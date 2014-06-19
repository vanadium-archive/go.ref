/*
 * Parse utilities for git statuses
 * @see http://git-scm.com/docs/git-status#_short_format
 * @fileoverview
 */

/*
 * Parses a single line of text produced by git status --short command
 * into an structured object representing the status of that file.
 * git-status short format is XY PATH1 -> PATH2
 * Please see documentation at:
 * http://git-scm.com/docs/git-status#_short_format
 * for details of the format and meaning of each component/
 * @param {string} gitStatusLine A single line of text produced
 * by git status --short command
 * @return {parser.item} A parsed object containing status, file path, etc..
 */
export function parse(gitStatusLine) {

  var validGitStatusRegex =  /^[\sMADRCU?!]{2}[\s]{1}.+/
  if(!validGitStatusRegex.test(gitStatusLine)) {
    throw new Error('Invalid git status line format. ' + gitStatusLine +
      ' does not match XY PATH1 -> PATH2 format where -> PATH2 is optional ' +
      ' and X and Y should be one of [<space>MADRCU?!].' +
      ' Please ensure you are using --short flag when running git status');
  }

  // X is status for working tree
  // Y is status for the index
  var X = gitStatusLine.substr(0,1);
  var Y = gitStatusLine.substr(1,1);

  // files
  var files = gitStatusLine.substring(3).split('->');
  var file = files[0];
  var oldFile = null;
  // we may have oldFile -> file for cases like rename and copy
  if (files.length == 2) {
    file = files[1];
    oldFile = files[0];
  }

  var fileStateInfo = getFileState(X, Y);
  var fileAction = getFileAction(X, Y , fileStateInfo.state);
  var summary = fileStateInfo.summary + ', file ' + fileAction;
  if(oldFile) {
    summary += ' to ' + oldFile;
  }

  return new item(
    fileAction,
    fileStateInfo.state,
    file,
    summary
  );
}

/*
 * A structure representing the status of a git file.
 * @param {string} fileAction, one of added, deleted, renamed, copied, modified, unknown
 * @param {string} fileState, one staged, notstaged, conflicted, untracked, ignored
 * @param {string} filePath filename and path
 * @param {string} summary A summary text for what these states mean
 * @class
 * @private
 */
class item {
  constructor(fileAction, fileState, filePath, summary) {
    this.fileAction = fileAction;
    this.fileState = fileState;
    this.filePath = filePath;
    this.summary = summary;
  }
}

var actionCodes = {
  'M': 'modified',
  'U': 'modified',
  'A': 'added',
  'D': 'deleted',
  'R': 'renamed',
  'C': 'copied',
  '?': 'unknown',
  '!': 'unknown'
}
/*
 * Returns the action performed on the file, one of added, deleted, renamed,
 * copied, modified, unknown
 * @private
 */
function getFileAction(X, Y, fileState) {
  var codeToUse = X;
  if (fileState === 'notstaged') {
    codeToUse = Y;
  }

  return actionCodes[codeToUse];
}

/*
 * Returns the git state of file, staged, notstaged, conflicted, untracked, ignored
 * @private
 */
function getFileState(X, Y) {

  // check for conflict, the following combinations represent a conflict
  // ------------------------------------------------
  //     D           D    unmerged, both deleted
  //     A           U    unmerged, added by us
  //     U           D    unmerged, deleted by them
  //     U           A    unmerged, added by them
  //     D           U    unmerged, deleted by us
  //     A           A    unmerged, both added
  //     U           U    unmerged, both modified
  // -------------------------------------------------
  var conflictSummary = null;
  if(X === 'D' && Y == 'D') {
    conflictSummary = 'Conflicted, both deleted';
  } else if(X === 'A' && Y === 'U') {
    conflictSummary = 'Conflicted, added by us';
  } else if(X === 'U' && Y === 'D') {
    conflictSummary = 'Conflicted, deleted by them';
  } else if(X === 'U' && Y === 'A') {
    conflictSummary = 'Conflicted, added by them';
  } else if(X === 'D' && Y === 'U') {
    conflictSummary = 'Conflicted, deleted by us';
  } else if(X === 'A' && Y === 'A') {
    conflictSummary = 'Conflicted, both added';
  } else if(X === 'U' && Y === 'U') {
    conflictSummary = 'Conflicted, both modified';
  }

  if (conflictSummary !== null) {
    return {
      state: 'conflicted',
      summary: conflictSummary
    }
  }

  // check for untracked
  if (X === '?' || Y === '?') {
    return {
      state:'untracked',
      summary: 'Untracked file'
    }
  }

  // check for ignored
  if (X === '!' || Y === '!') {
    return {
      state: 'ignored',
      summary: 'Ignored file'
    }
  }

  // check for notstaged
  if (Y !== ' ') {
    return {
      state:'notstaged',
      summary: 'Not staged for commit'
    }
  } else {
    // otherwise staged
    return {
      state: 'staged',
      summary: 'Staged for commit'
    }
  }
}

