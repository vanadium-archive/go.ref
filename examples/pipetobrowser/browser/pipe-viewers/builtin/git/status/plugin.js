/*
 * git/status is a Pipe Viewer that displays output of "git status --short"
 * in a graphical grid.
 * Only supported with Git's --short  flag.
 * @tutorial git status --short | p2b google/p2b/[name]/git/status
 * @fileoverview
 */
import { View } from 'view';
import { PipeViewer } from 'pipe-viewer';

import { parse } from './parser';

var streamUtil = require('event-stream');

class GitStatusPipeViewer extends PipeViewer {
  get name() {
    return 'git/status';
  }

  play(stream) {
    var listView = document.createElement('ul');

    // TODO(aghassemi) let's have the plugin specify if they expect data in
    // in binary or text so p2b can set the proper encoding for them rather
    // than each plugin doing it like this.
    // read data as UTF8
    stream.setEncoding('utf8');

    // split by new line
    stream = stream.pipe(streamUtil.split(/\r?\n/));

    var statusItems = [];
    var statusView = document.createElement('p2b-plugin-git-status');
    statusView.statusItems = statusItems;

    stream.on('data', (line) => {
      if (line.trim().length > 0) {
        statusItems.push( parse(line) );
      }
    });

    return new View(statusView);
  }
}

export default GitStatusPipeViewer;