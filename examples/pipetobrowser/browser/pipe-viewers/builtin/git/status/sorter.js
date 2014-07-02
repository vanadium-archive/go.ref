var stateSortPriority = {
  'conflicted' : 1,
  'untracked' : 2,
  'notstaged' : 3,
  'staged': 4,
  'ignored': 5
}

var actionSortPriority = {
  'added' : 1,
  'deleted' : 2,
  'modified' : 3,
  'renamed': 4,
  'copied': 5,
  'unknown': 6
}

export function gitStatusSort(item1, item2, key, ascending) {
  var first = item1[key];
  var second = item2[key];
  if (!ascending) {
    first = item2[key];
    second = item1[key];
  }

  if (key === 'state') {
    first = stateSortPriority[first];
    second = stateSortPriority[second];
  }

  if (key === 'action') {
    first = actionSortPriority[first];
    second = actionSortPriority[second];
  }

  if (typeof first === 'string') {
    return first.localeCompare(second);
  } else {
    return first - second;
  }
};