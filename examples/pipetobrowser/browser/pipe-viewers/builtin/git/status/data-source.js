/*
 * Implement the data source which handles searching, filtering
 * and sorting of the git status items
 * @fileoverview
 */

import { gitStatusSort } from './sorter';
import { gitStatusSearch } from './searcher';
import { gitStatusFilter } from './filterer';

export class gitStatusDataSource {
  constructor(items) {

    /*
     * all items, unlimited buffer for now.
     * @private
     */
    this.allItems = items;
  }

  /*
   * Implements the fetch method expected by the grid components.
   * handles searching, filtering and sorting of the data.
   * search, sort and filters are provided by the grid control whenever they are
   * changed by the user.
   * DataSource is called automatically by the grid when user interacts with the component
   * Grid does some batching of user actions and only calls fetch when needed.
   * keys provided for sort and filters correspond to keys set in the markup
   * when constructing the grid.
   * @param {object} search search{key<string>} current search keyword
   * @param {object} sort sort{key<string>, ascending<bool>} current sort key and direction
   * @param {map} filters map{key<string>, values<Array>} Map of filter keys to currently selected filter values
   * @return {Array<object>} Returns an array of filtered sorted results of the items.
   */
  fetch(search, sort, filters) {

    var filteredSortedItems = this.allItems.
      filter((item) => {
        return gitStatusFilter(item, filters);
      }).
      filter((item) => {
        return gitStatusSearch(item, search.keyword);
      }).
      sort((item1, item2) => {
        return gitStatusSort(item1, item2, sort.key, sort.ascending);
      });

    return filteredSortedItems;

  }
}