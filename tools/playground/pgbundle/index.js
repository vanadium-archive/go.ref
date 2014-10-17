var _ = require('lodash');
var fs = require('fs');
var glob = require('glob');
var path = require('path');

// Filename to write the data to.
var BUNDLE_NAME = 'bundle.json';

module.exports = {run: run};

// TODO(nlacasse): improve this.
function usage() {
  console.log('Usage: pgbundle [options] <path> [<path> <path> ...]');
  console.log('Options: --verbose: Enable verbose output.');
  process.exit(1);
}

// If the first line is "// +build OMIT", strip the line and return the
// remaining lines.
function stripBuildOmit(lines) {
  if (lines[0] === '// +build OMIT') {
    return _.rest(lines);
  }
  return lines;
}

// If the first line is an index comment, strip the line and return the index
// and remaining lines.
function getIndex(lines) {
  var index = null;
  var match = lines[0].match(/^\/\/\s*index=(\d+)/);
  if (match && match[1]) {
    index = match[1];
    lines = _.rest(lines);
  }
  return {
    index: index,
    lines: lines
  };
}

function shouldIgnore(fileName) {
  // Ignore directories.
  if (_.last(fileName) === '/') {
    return true;
  }
  // Ignore bundle files.
  if (fileName === BUNDLE_NAME) {
    return true;
  }
  // Ignore generated .vdl.go files.
  if ((/\.vdl\.go$/i).test(fileName)) {
    return true;
  }
  return false;
}

// Main function.
function run() {
  // Get the paths from process.argv.
  var argv = require('minimist')(process.argv.slice(2));
  var dirs = argv._;

  // Make sure there is at least one path.
  if (!dirs || dirs.length === 0) {
    return usage();
  }

  // Loop over each path.
  _.each(dirs, function(dir) {
    var subFiles = glob.sync('**', {
      cwd: dir,
      mark: true  // Add a '/' char to directory matches.
    });

    if (subFiles.length === 0) {
      return usage();
    }

    var out = {files: []};

    // Loop over each subfile in the path.
    _.each(subFiles, function(fileName) {
      if (shouldIgnore(fileName)) {
        return;
      }

      var fullFilePath = path.resolve(dir, fileName);
      var text = fs.readFileSync(fullFilePath, {encoding: 'utf8'});

      var lines = text.split('\n');
      lines = stripBuildOmit(lines);
      var indexAndLines = getIndex(lines);
      var index = indexAndLines.index;
      lines = indexAndLines.lines;

      // TODO(sadovsky): Should we put the index in the bundle? Note that we
      // already order files by index below. The playground client currently
      // does not use the index.
      out.files.push({
        name: path.basename(fileName),
        body: lines.join('\n'),
        index: index
      });
    });

    out.files = _.sortBy(out.files, 'index');

    // Write the bundle.json.
    var outFile = path.resolve(dir, BUNDLE_NAME);
    fs.writeFileSync(outFile, JSON.stringify(out));

    if (argv.verbose) {
      console.log('Wrote ' + outFile);
    }
  });
}
