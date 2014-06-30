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
import { vLogDataSource } from './data-source';
import { Logger } from 'logger'

var log = new Logger('pipe-viewers/builtin/vlog');

var streamUtil = require('event-stream');

class vLogPipeViewer extends PipeViewer {
  get name() {
    return 'vlog';
  }

  play(stream) {
    stream.setEncoding('utf8');

    // split by new line
    stream = stream.pipe(streamUtil.split(/\r?\n/));
    var logView = document.createElement('p2b-plugin-vlog');

    // create a new data source from the stream and set it.
    logView.dataSource = new vLogDataSource(
      stream,
      function onNewItem() {
        // also refresh the grid when new data comes in.
        // grid component batches requests and refreshes UI on the next animation frame.
        logView.refreshGrid();
      },
      function onError(err) {
        log.debug(err);
      });

    // also refresh the grid when new data comes in.
    // grid component batches requests and refreshes UI on the next animation frame.
    stream.on('data', () => {
      logView.refreshGrid();
    });

    return new View(logView);
  }
}

export default vLogPipeViewer;