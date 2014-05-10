// Provides tasks to spawn and kill subprocesses needed for todos app.
// See also: javascript/api/grunt_tasks/integration_test_environment_setup.js

var assert = require('assert');
var fs = require('fs');
var spawn = require('child_process').spawn;

module.exports = function(grunt) {
  var subprocesses = [];  // List of subprocesses we've spawned.

  // Kills all subprocesses we've spawned.
  var tearDown = function() {
    subprocesses.forEach(function(p) {
      p.kill('SIGTERM');
    });
  };

  // Calls tearDown, then fails the current task.
  var fail = function(msg) {
    tearDown();
    grunt.fail.fatal(msg);
    done(false);
  };

  // Starts all subprocesses and sets some grunt.config vars.
  grunt.registerTask('subtask_spawnSubprocesses', function() {
    // Tell grunt that this is an async task.
    // Note, we never actually call done() -- see comment below.
    var done = this.async();

    // Define constants.
    var c = grunt.constants;
    var VEYRON_IDENTITY_BIN = c.VEYRON_BIN_DIR + '/identityd';
    var VEYRON_PROXY_BIN = c.VEYRON_BIN_DIR + '/proxyd';
    var VEYRON_WSPR_BIN = c.VEYRON_BIN_DIR + '/wsprd';
    var STORE_BIN = c.VEYRON_BIN_DIR + '/todos_stored';
    var VEYRON_PROXY_ADDR = '127.0.0.1:' + c.VEYRON_PROXY_PORT;

    // Ensure binaries exist.
    var bins = [VEYRON_IDENTITY_BIN, VEYRON_PROXY_BIN, VEYRON_WSPR_BIN];
    bins.forEach(function(bin) {
      if (!fs.existsSync(bin)) {
        fail('Missing binary: ' + bin);
      }
    });

    // Start subprocesses.
    var startSubprocess = function(command, args) {
      var p = spawn(command, args);
      p.stderr.on('data', grunt.log.debug);
      subprocesses.push(p);
      return p;
    };
    var veyronIdentitySubprocess = startSubprocess(
        VEYRON_IDENTITY_BIN,
        ['-port=' + c.VEYRON_IDENTITY_PORT]);
    var veyronProxySubprocess = startSubprocess(
        VEYRON_PROXY_BIN,
        ['-log_dir=' + c.LOG_DIR, '-addr=' + VEYRON_PROXY_ADDR]);
    var veyronWsprSubprocess = startSubprocess(
        VEYRON_WSPR_BIN,
        ['-v=3', '-log_dir=' + c.LOG_DIR, '-port=' + c.VEYRON_WSPR_PORT,
         '-vproxy=' + VEYRON_PROXY_ADDR]);
    var storeSubprocess = startSubprocess(
        STORE_BIN, ['-log_dir=' + c.LOG_DIR]);

    // Capture todos_stored's endpoint, then write env.js file.
    storeSubprocess.stdout.on('data', function(data) {
      var endpoint;
      data.toString().split('\n').some(function(line) {
        line = line.replace('Endpoint: ', '').trim();
        if (line.match(/^@.*@$/)) {
          endpoint = line;
          return true;
        }
      });
      assert(endpoint, 'Failed to extract store endpoint');

      // Write JS file that sets env vars for client-side JS.
      var env = {
        VEYRON_WSPR_SERVER_URL: 'http://localhost:' + c.VEYRON_WSPR_PORT,
        STORE_ENDPOINT: endpoint
      };
      console.log(JSON.stringify(env, null, 2));
      var code = 'var env = JSON.parse(\'' + JSON.stringify(env) + '\');';
      // TODO(sadovsky): This is super hacky -- we shouldn't be writing files
      // into the webserver's dir. It'd be better to communicate these vars to
      // todos_appd via env vars or flags.
      grunt.file.write('todos_appd/js/env.js', code);

      console.log('Now run this: ./go/bin/todos_appd');
    });

    // Note, we do not call done() here, because we want to block until the user
    // hits Ctrl-C. We rely on the Node process exit handler to kill our spawned
    // subprocesses.
  });

  // Kills all subprocesses we started. Not currently used, since spawn blocks.
  grunt.registerTask('subtask_killSubprocesses', function() {
    tearDown();
  });

  process.on('exit', function() {
    tearDown();
  });
};
