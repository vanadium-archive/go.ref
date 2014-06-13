/*
 * Creates a string, given a sequence of UTF-8 bytes.
 * @param {Uint8Array, Array.<int>} buf array of bytes that represent a UTF-8 string
 */
export function UTF8BytesToString(buf) {
  var encoded = String.fromCharCode.apply(null, buf);
  var decoded = decodeURIComponent(escape(encoded))
  return decoded;
}