export function vLogSort(logItem1, logItem2, key, ascending) {
  var first = logItem1;
  var second = logItem2;
  if (!ascending) {
    first = logItem2;
    second = logItem1;
  }

  if (typeof first[key] === 'string') {
    return first[key].localeCompare(second[key]);
  } else {
    return first[key] - second[key];
  }
};