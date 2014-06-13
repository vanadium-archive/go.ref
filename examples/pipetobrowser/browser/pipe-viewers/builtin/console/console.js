/*
 * Console is a Pipe Viewer that displays a text stream as unformatted text
 * @fileoverview
 */

import { View } from 'view';
import { PipeViewer } from 'pipe-viewer';

import { UTF8BytesToString } from 'libs/utils/encoding'

class ConsolePipeViewer extends PipeViewer {
  get name() {
    return 'console';
  }

  play(stream) {
    var console = document.createElement('p2b-plugin-console');
    stream.on('data', (chunk) => {
      var buf = UTF8BytesToString(new Uint8Array(chunk));
      console.addText(buf);
    });

    return new View(console);
  }
}

export default ConsolePipeViewer;