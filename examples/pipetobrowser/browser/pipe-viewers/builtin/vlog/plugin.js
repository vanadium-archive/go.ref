/*
 * vlog is a Pipe Viewer that displays veyron logs in a graphical grid.
 * Please note that Veyron writes logs to stderr stream, in *nix systems 2>&1
 * can be used to redirect stderr to stdout which can be then piped to P2B.
 * @tutorial myVeyronServerd -v=3 2>&1 | p2b google/p2b/[name]/vlog
 * @tutorial cat logfile.txt | p2b google/p2b/[name]/vlog
 * @fileoverview
 */
import { View } from 'view';
import { PipeViewer } from 'pipe-viewer';
import { streamUtil } from 'stream-helpers';
import { formatDate } from 'formatting';
import { Logger } from 'logger'
import { vLogDataSource } from './data-source';

var log = new Logger('pipe-viewers/builtin/vlog');

class vLogPipeViewer extends PipeViewer {
  get name() {
    return 'vlog';
  }

  play(stream) {
    stream.setEncoding('utf8');

    var logView = document.createElement('p2b-plugin-vlog');
    var newData = true;
    var refreshGrid = function() {
      requestAnimationFrame(() => {
        if( newData ) {
          logView.refreshGrid();
          newData = false;
        }
        refreshGrid();
      });
    };
    refreshGrid();

    // split by new line
    stream = stream.pipe(streamUtil.split(/\r?\n/));

    // create a new data source from the stream and set it.
    logView.dataSource = new vLogDataSource(
      stream,
      function onNewItem(item) {
        newData = true;
        // add additional, UI related properties to the item
        addAdditionalUIProperties(item);
      },
      function onError(err) {
        log.debug(err);
      });

    return new View(logView);
  }
}

/*
 * Adds additional UI specific properties to the item
 * @private
 */
function addAdditionalUIProperties(item) {
  addIconProperty(item);
  addFormattedDate(item);
}

/*
 * Adds an icon property to the item specifying what icon to display
 * based on log level
 * @private
 */
function addIconProperty(item) {
  var iconName = 'info';
  switch (item.level) {
    case 'info':
      iconName = 'info-outline';
      break;
    case 'warning':
      iconName = 'warning';
      break;
    case 'error':
      iconName = 'info';
      break;
    case 'fatal':
      iconName = 'block';
      break;
  }

  item.icon = iconName;
}

/*
 * Adds a human friendly date field
 * @private
 */
function addFormattedDate(item) {
  item.formattedDate = formatDate(item.date);
}

export default vLogPipeViewer;