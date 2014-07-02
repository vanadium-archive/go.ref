/*
 * git/status is a Pipe Viewer that displays output of "git status --short"
 * in a graphical grid.
 * Only supported with Git's --short  flag.
 * @tutorial git status --short | p2b google/p2b/[name]/git/status
 * @fileoverview
 */
import { View } from 'view';
import { PipeViewer } from 'pipe-viewer';
import { Logger } from 'logger'
import { parse } from './parser';
import { gitStatusDataSource } from './data-source';

var log = new Logger('pipe-viewers/builtin/git/status');

var streamUtil = require('event-stream');

class GitStatusPipeViewer extends PipeViewer {
  get name() {
    return 'git/status';
  }

  play(stream) {
    stream.setEncoding('utf8');

    // split by new line
    stream = stream.pipe(streamUtil.split(/\r?\n/));

    // parse the git status items
    stream = stream.pipe(streamUtil.map((line, cb) => {
      if (line.trim() === '') {
        // eliminate the item
        cb();
        return;
      }
      var item;
      try {
        item = parse(line);
      } catch(e) {
        log.debug(e);
      }
      if (item) {
        addAdditionalUIProperties(item);
        cb(null, item);
      } else {
        // eliminate the item
        cb();
      }
    }));

    // we return a view promise instead of a view since we want to wait
    // until all items arrive before showing the data.
    var viewPromise = new Promise(function(resolve,reject) {
      // write into an array when stream is done return the UI component
      stream.pipe(streamUtil.writeArray((err, items) => {
        if (err) {
          reject(err);
        } else {
          var statusView = document.createElement('p2b-plugin-git-status');
          statusView.dataSource = new gitStatusDataSource(items);
          resolve(new View(statusView));
        }
      }));
    });

    return viewPromise;
  }
}

/*
 * Adds additional UI specific properties to the item
 * @private
 */
function addAdditionalUIProperties(item) {
  addActionIconProperty(item);
  addStateIconProperty(item);
  addFileNameFileParentProperty(item);
}

/*
 * Adds an icon property to the item specifying what icon to display
 * based on state
 * @private
 */
function addStateIconProperty(item) {
  var iconName;
  switch (item.state) {
    case 'staged':
      iconName = 'check-circle';
      break;
    case 'notstaged':
      iconName = 'warning';
      break;
    case 'conflicted':
      iconName = 'error';
      break;
    case 'untracked':
      iconName = 'report';
      break;
    case 'ignored':
      iconName = 'visibility-off';
      break;
  }

  item.stateIcon = iconName;
}

/*
 * Adds an icon property to the item specifying what icon to display
 * based on action
 * @private
 */
function addActionIconProperty(item) {
  var iconName;
  switch (item.action) {
    case 'added':
      iconName = 'add';
      break;
    case 'deleted':
      iconName = 'clear';
      break;
    case 'modified':
      iconName = 'translate';
      break;
    case 'renamed':
      iconName = 'sync';
      break;
    case 'copied':
      iconName = 'content-copy';
      break;
    case 'unknown':
      iconName = 'remove';
      break;
  }

  item.actionIcon = iconName;
}

/*
 * Splits file into filename and fileParent
 * @private
 */
function addFileNameFileParentProperty(item) {

  var filename = item.file;
  var fileParent = "./";

  var slashIndex = item.file.lastIndexOf('/');

  if (slashIndex > 0) {
    filename = item.file.substr(slashIndex + 1);
    fileParent = item.file.substring(0, slashIndex);
  }

  item.filename = filename;
  item.fileParent = fileParent;
}

export default GitStatusPipeViewer;