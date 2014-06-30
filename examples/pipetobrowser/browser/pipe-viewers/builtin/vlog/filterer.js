/*
 * Returns whether the given vLogItem matches the map of filters.
 * @param {Object} item A single veyron log item as defined by parser.item
 * @param {map} filters Map of keys to selected filter values as defined
 * when constructing the filters in the grid components.
 * e.g. filters:{'date':'all', 'levels':['info','warning']}
 * @return {boolean} Whether the item satisfies ALL of the given filters.
 */
export function vLogFilter(item, filters) {
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
 * Returns whether the given vLogItem matches a single filter
 * @param {Object} item A single veyron log item as defined by parser.item
 * @param {string} key filter key e.g. 'date'
 * @param {string} value filter value e.g. 'all'
 * @return {boolean} Whether the item satisfies then the given filter key value pair
 * @private
 */
function applyFilter(item, key, value) {
  switch (key) {
    case 'date':
      return filterDate(item, value);
    case 'level':
      return filterLevel(item, value);
    default:
      // ignore unknown filters
      return true;
  }
}

/*
 * Returns whether item's date satisfies the date filter
 * @param {Object} item A single veyron log item as defined by parser.item
 * @param {string} since One of 'all', '1' or '24'. for Anytime, past one hour, past 24 hours.
 * @return {boolean} whether item's date satisfies the date filter
 * @private
 */
function filterDate(item, since) {
  if (since === 'all') {
    return true;
  } else {
    var hours = parseInt(since);
    var targetDate = new Date();
    targetDate.setHours(targetDate.getHours() - hours);
    return item.date > targetDate;
  }
}

/*
 * Returns whether item's level is one of the given values
 * @param {Object} item A single veyron log item as defined by parser.item
 * @param {Array<string>} levels Array of level values e.g. ['info','warning']
 * @return {boolean} whether item's level is one of the given values
 * @private
 */
function filterLevel(item, levels) {
  return levels.indexOf(item.level) >= 0;
}
