/*
 * Logger represents a module that can write logging messages to the console.
 * @param {string} prefix A string that will be prefixed to every log message
 * @class
 */
export class Logger {
  constructor(prefix) {
    prefix = prefix || 'Error';
    this.prefix_ = prefix;
  }

  debug(...args) {
    console.log('DEBUG: ' + this.prefix_ + ':', ...args);
  }
};
