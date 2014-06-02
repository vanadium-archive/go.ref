/*
 * Given a collection of objects, returns true if all of them exist
 * Returns false as soon as one does not exist.
 * @param {*} [...] objects Objects to check existence of
 * @return {bool} Whether all of the given objects exist or not
 */
export function exists(...objects) {
  for (var obj of objects) {
    if (typeof obj === 'undefined' || obj === null) {
      return false;
    }
  }

  return true;
}
