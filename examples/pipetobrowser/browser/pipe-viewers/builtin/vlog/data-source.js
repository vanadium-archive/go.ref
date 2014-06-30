/*
 * Implement the data source which handles parsing the stream, searching, filtering
 * and sorting of the vLog items.
 * @fileoverview
 */
import { parse } from './parser';
import { vLogSort } from './sorter';
import { vLogSearch } from './searcher';
import { vLogFilter } from './filterer';

export class vLogDataSource {
  constructor(stream, onNewItem, onError) {

    /*
     * all logs, unlimited buffer for now.
     * @private
     */
    this.allLogItems = [];

    /*
     * Raw stream of log data
     * @private
     */
    this.stream = stream;

    stream.on('data', (line) => {
      if (line.trim() === '') {
        return;
      }
      var logItem = null;
      // try to parse and display as much as we can.
      try {
        logItem = parse(line);
      } catch (e) {
        if (onError) {
          onError(e);
        }
      }

      if (logItem) {
        this.allLogItems.push(logItem);
        if (onNewItem) {
          onNewItem(logItem);
        }
      }
    });

  }

  /*
   * Implements the fetch method expected by the grid components.
   * handles parsing the stream, searching, filtering and sorting of the data.
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

    var filteredSortedItems = this.allLogItems.
    filter((item) => {
      return vLogFilter(item, filters);
    }).
    filter((item) => {
      return vLogSearch(item, search.keyword);
    }).
    sort((item1, item2) => {
      return vLogSort(item1, item2, sort.key, sort.ascending);
    });

    // pause or resume the stream when auto-refresh filter changes
    if (filters['autorefresh'] && this.stream.paused) {
      this.stream.resume();
    }
    if (!filters['autorefresh'] && !this.stream.paused) {
      this.stream.pause();
    }

    return filteredSortedItems;
  }
}