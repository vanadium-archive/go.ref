/*
 * dev/null simply consumes the stream without taking any action on the data
 * or keeping it in memory
 * @tutorial echo "To the black hole!" | p2b google/p2b/[name]/dev/null
 * @fileoverview
 */

import { View } from 'view';
import { PipeViewer } from 'pipe-viewer';

class DevNullPipeViewer extends PipeViewer {
  get name() {
    return 'dev/null';
  }

  play(stream) {

    var blackhole = document.createElement('p2b-ui-components-blackhole');
    blackhole.start();

    stream.on('data', () => {
      // consume the stream
    });

    stream.on('end', () => {
      blackhole.stop();
    });

    return new View(blackhole);
  }
}

export default DevNullPipeViewer;