/*
 * vlog is a Pipe Viewer that displays veyron logs in a graphical grid.
 * Please note that Veyron writes logs to stderr stream, in *nix systems 2>&1
 * can be used to redirect stderr to stdout which can be then piped to P2B.
 * @tutorial myVeyronServerd -v=3 2>&1 | p2b google/p2b/[name]/vlog
 * @tutorial cat logfile.text | p2b google/p2b/[name]/vlog
 * @fileoverview
 */
import { View } from 'view';
import { PipeViewer } from 'pipe-viewer';

import { parse } from './parser';

var streamUtil = require('event-stream');

class vLogPipeViewer extends PipeViewer {
  get name() {
    return 'vlog';
  }

  play(stream) {
    stream.setEncoding('utf8');

    // split by new line
    stream = stream.pipe(streamUtil.split(/\r?\n/));

    var logItems = [];
    var logView = document.createElement('p2b-plugin-vlog');
    logView.logItems = logItems;

    stream.on('data', (line) => {
      // try to parse and display as much as we can.
      try {
        logItems.push( parse(line) );
      } catch(e) {}

    });

    return new View(logView);
  }
}

export default vLogPipeViewer;