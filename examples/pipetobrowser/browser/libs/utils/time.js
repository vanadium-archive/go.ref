/*
 * Given a time duration in seconds, formats it as h' hours, m' minutes, s' seconds
 * in EN-US.
 * @param {integer} durationInSeconds Time period in seconds
 * @return {string} EN-US formatted time period.
 */
export function formatDuration(durationInSeconds) {
  var hours = Math.floor(durationInSeconds/3600);
  var minutes = Math.floor((durationInSeconds - (hours*3600))/60);
  var seconds = durationInSeconds - (hours*3600) - (minutes*60);

  return _pluralize('hour', hours, true) +
  _pluralize('minute', minutes, true) +
  _pluralize('second', seconds, false);
}

function _pluralize(name, value, returnEmptyIfZero) {
  if(value == 0 && returnEmptyIfZero) {
    return '';
  }
  if(value != 1) {
    name = name + 's';
  }
  return value + ' ' + name + ' ';
}