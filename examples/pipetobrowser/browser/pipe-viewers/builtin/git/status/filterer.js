/*
 * Returns whether the given git status items matches the map of filters.
 * @param {Object} item A single git status item as defined by parser.item
 * @param {map} filters Map of keys to selected filter values as defined
 * when constructing the filters in the grid components.
 * e.g. filters:{'state':['staged'], 'action':['added','modified']}
 * @return {boolean} Whether the item satisfies ALL of the given filters.
 */
export function gitStatusFilter(item, filters) {
  if (Object.keys(filters).length === 0) {
    return true;
  }

  for (var key in filters) {
    var isMatch = applyFilter(item, key, filters[key]);
    // we AND all the filters, short-circuit for early termination
    if (!isMatch) {
      return false;
    }
  }

  // matches all filters
  return true;
};

/*
 * Returns whether the given git status item matches a single filter
 * @param {Object} item A single git status item as defined by parser.item
 * @param {string} key filter key e.g. 'state'
 * @param {string} value filter value e.g. '['staged','untracked']
 * @return {boolean} Whether the item satisfies then the given filter key value pair
 * @private
 */
function applyFilter(item, key, value) {
  switch (key) {
    case 'state':
    case 'action':
      return value.indexOf(item[key]) >= 0;
    default:
      // ignore unknown filters
      return true;
  }
}