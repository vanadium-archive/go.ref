var Transform = require('stream').Transform;
var Buffer = require('buffer').Buffer;
// TODO(aghassemi) doesn't look like ES6 and CommonJS modules can use the same
// syntax to be referenced, but research more, maybe something can be done at
// built time.

/*
 * Adapts a stream of byte arrays in object mode to a regular stream of Buffer
 * @class
 */
export class ByteObjectStreamAdapter extends Transform {
  constructor() {
    super();
    this._writableState.objectMode = true;
    this._readableState.objectMode = false;
  }

  _transform(bytesArr, encoding, cb) {
    var buf = new Buffer(new Uint8Array(bytesArr));
    this.push(buf);

    cb();
  }
}
