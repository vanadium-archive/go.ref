/*
 * Console is a Pipe Viewer that displays a text stream as unformatted text
 * @tutorial echo "Hello World" | p2b google/p2b/[name]/console
 * @fileoverview
 */

import { View } from 'view';
import { PipeViewer } from 'pipe-viewer';

class ConsolePipeViewer extends PipeViewer {
  get name() {
    return 'console';
  }

  play(stream) {
    var consoleView = document.createElement('p2b-plugin-console');

    // read data as UTF8
    stream.setEncoding('utf8');
    stream.on('data', (buf) => {
      var textVal = buf.toString();
      consoleView.addText(textVal);
    });

    return new View(consoleView);
  }
}

export default ConsolePipeViewer;