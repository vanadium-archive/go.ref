/*
 * Bootstrapping traceur compiler, polymer, and the app itself.
 * @fileoverview
 */

window.addEventListener('polymer-ready', function(e) {
  // alias view and pipe-viewer so external plugins can just reference it without full path.
  System.paths = {
    '*': '*.js',
    'pipe-viewer': 'pipe-viewers/pipe-viewer.js',
    'pipe-viewer-delegation': 'pipe-viewers/pipe-viewer-delegation.js',
    'view': 'libs/mvc/view.js'
  };

  System.import('runtime/app').then(function(app) {
    app.start();
  }).catch(function(e) {
    console.error(e);
  });
});
