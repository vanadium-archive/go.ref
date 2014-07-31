/*
 * Console is a Pipe Viewer that displays a text stream as unformatted text
 * @tutorial echo "Hello World" | p2b google/p2b/[name]/console/panic
 * @fileoverview
 */

import { View } from 'view';
import { PipeViewer } from 'pipe-viewer';

class ConsolePanicPipeViewer extends PipeViewer {
  get name() {
    return 'console-panic';
  }

  play(stream) {
    var consoleView = document.createElement('p2b-plugin-console-panic');

    // Internal stream variables
    var panicked = false;
    var lastPiece = "";

    // read data as UTF8
    stream.setEncoding('utf8');
    stream.on('data', (buf) => {
      // Always split the incoming text by newline.
      var pieces = buf.toString().split("\n");
      for (var i = 0; i < pieces.length; i++) {
        var piece = pieces[i];

        // flags for the status of the current line
        var goroutine = (piece.search(/goroutine \d+ \[\D+\]:/) == 0);
        var startOfLine = (i != 0 || lastPiece == "");

        if (goroutine && startOfLine) {
          // We spotted a goroutine at the start of a line, so create a new container.
          consoleView.startNewContainer(panicked, piece);

          // Stop scrolling if we've already panicked.
          // This will focus the element on the first crashed goroutine.
          if (panicked) {
            consoleView.scrollAuto();
            consoleView.scrollOff();
          }
          panicked = true;

          // We also need a new line for our new container.
          consoleView.startNewLine();
        } else if (i > 0) {
          // These lines followed a \n in the buffer and thus should go on a new line.
          if (lastPiece == "") {
            // The previous line was empty; empty div's have 0 height.
            // We need to add a <br /> to print the linebreak properly.
            consoleView.addLineBreak();
          }
          consoleView.startNewLine();
        }

        // Add the relevant text.
        consoleView.addText(piece);
        lastPiece = piece;
      }
    });

    return new View(consoleView);
  }
}

export default ConsolePanicPipeViewer;