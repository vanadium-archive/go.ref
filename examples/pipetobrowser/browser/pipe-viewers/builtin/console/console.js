/*
 * Console is a Pipe Viewer that displays a text stream as unformatted text
 * @fileoverview
 */

import { View } from 'view';
import { PipeViewer } from 'pipe-viewer';

import { arrayBufferToString } from 'libs/utils/array-buffer'

class ConsolePipeViewer extends PipeViewer {
  get name() {
    return 'console';
  }

  play(stream) {
    var textarea = document.createElement('textarea');
    textarea.readonly = true;
    textarea.cols = 100;
    textarea.rows = 15;
    var chunk;
    stream.on('readable', () => {
      while(null !== (chunk = stream.read())) {
        var buf = arrayBufferToString(chunk);
        textarea.value += buf;
      }
    });

    return new View(textarea);
  }
}

export default ConsolePipeViewer;