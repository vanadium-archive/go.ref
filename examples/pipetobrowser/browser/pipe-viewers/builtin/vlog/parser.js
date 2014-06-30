/*
 * Parse utilities for veyron logs
 * @fileoverview
 */

/*
 * Parses a single line of text produced by veyron logger
 * into an structured object representing it.
 * Log lines have this form:
 * Lmmdd hh:mm:ss.uuuuuu threadid file:line] msg...
 * where the fields are defined as follows:
 *  L                A single character, representing the log level (eg 'I' for INFO)
 *  mm               The month (zero padded; ie May is '05')
 *  dd               The day (zero padded)
 *  hh:mm:ss.uuuuuu  Time in hours, minutes and fractional seconds
 *  threadid         The space-padded thread ID as returned by GetTID()
 *  file             The file name
 *  line             The line number
 *  msg              The user-supplied message
 * @param {string} vlogLine A single line of veyron log
 * @return {parser.item} A parsed object containing log level, date, file,
 * line number, thread id and message.
 */
export function parse(vlogLine) {

  var validLogLineRegEx = /^([IWEF])(\d{2})(\d{2})\s(\d{2}:\d{2}:\d{2}\.\d+)\s(\d+)\s(.*):(\d+)]\s+(.*)$/
  var logParts = vlogLine.match(validLogLineRegEx);
  if (!logParts || logParts.length != 8 + 1) { // 8 parts + 1 whole match
    throw new Error('Invalid vlog line format. ' + vlogLine +
      ' Lmmdd hh:mm:ss.uuuuuu threadid file:line] msg.. pattern');
  }

  var L = logParts[1];
  var month = logParts[2];
  var day = logParts[3];
  var time = logParts[4];
  var treadId = parseInt(logParts[5]);
  var file = logParts[6];
  var fileLine = parseInt(logParts[7]);
  var message = logParts[8];

  var now = new Date();
  var year = now.getFullYear();
  var thisMonth = now.getMonth() + 1; // JS months are 0-11
  // Year flip edge case, if log month > this month, we assume log line is from previous year
  if (parseInt(month) > thisMonth) {
    year--;
  }

  var date = new Date(year + '-' + month + '-' + day + ' ' + time);

  return new item(
    levelCodes[L],
    date,
    treadId,
    file,
    fileLine,
    message
  );
}

var levelCodes = {
  'I': 'info',
  'W': 'warning',
  'E': 'error',
  'F': 'fatal'
}

/*
 * A structure representing a veyron log item
 * @param {string} level, one of info, warning, error, fatal
 * @param {date} date, The date and time of the log item
 * @param {integer} threadId The thread ID as returned by GetTID()
 * @param {string} file The file name
 * @param {integer} fileLine The file line number
 * @param {string} message The user-supplied message
 * @class
 * @private
 */
class item {
  constructor(level, date, threadId, file, fileLine, message) {
    this.level = level;
    this.date = date;
    this.threadId = threadId;
    this.file = file;
    this.fileLine = fileLine;
    this.message = message;
  }
}