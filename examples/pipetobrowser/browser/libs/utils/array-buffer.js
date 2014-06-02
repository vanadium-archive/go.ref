/*
 * Creates a string, given a sequence of bytes. It assumes UTF-8 encoding.
 * @param {ArrayBuffer} buf array of bytes that represent a UTF-8 string
 */
export function arrayBufferToString(buf) {
  var encoded = String.fromCharCode.apply(null, buf);
  var decoded = decodeURIComponent(escape(encoded))
  return decoded;
}