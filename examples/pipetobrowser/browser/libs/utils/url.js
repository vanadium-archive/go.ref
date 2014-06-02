var hasProtocol = new RegExp('^(?:[a-z]+:)?//', 'i');

/*
 * Decides whether a string is an absolute Url by seeing if it starts with a protocol.
 * @param {string} val string value to check.
 * @return {bool} whether or not the string value is a Url
 */
export function isAbsoulteUrl(val) {
  return hasProtocol.test(val);
}