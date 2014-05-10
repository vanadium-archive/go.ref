var assert = require('assert');
var fs = require('fs');
var path = require('path');

module.exports = function(grunt) {
  require('load-grunt-tasks')(grunt);
  grunt.task.loadTasks('grunt_tasks');

  grunt.initConfig({
    pkg: grunt.file.readJSON('package.json'),
    jshint: {
      files: ['**/*.js'],
      options: {
        ignores: ['**/veyron*.js',
                  'node_modules/**/*.js']
      }
    }
  });

  var vIndex = __dirname.indexOf('/v/');
  assert.notEqual(vIndex, -1, 'Failed to find Veyron root dir');

  grunt.constants = {
    LOG_DIR: path.resolve('log'),
    VEYRON_BIN_DIR: __dirname.substr(0, vIndex) + '/v/bin',
    VEYRON_IDENTITY_PORT: 3000,
    VEYRON_PROXY_PORT: 3001,
    VEYRON_WSPR_PORT: 3002
  };
  var c = grunt.constants;

  // Make dirs as needed.
  [c.LOG_DIR].forEach(function(dir) {
    if (!fs.existsSync(dir)) fs.mkdirSync(dir);
  });

  // Starts all needed daemons and blocks. On Ctrl-C, kills all spawned
  // subprocesses and then exits.
  grunt.registerTask('start', [
    'subtask_spawnSubprocesses'
  ]);
};
