System.config({
  "paths": {
    "*": "*.js",
    "pipe-viewer": "pipe-viewers/pipe-viewer.js",
    "pipe-viewer-delegation": "pipe-viewers/pipe-viewer-delegation.js",
    "view": "libs/mvc/view.js",
    "logger": "libs/logs/logger.js",
    "stream-helpers": "libs/utils/stream-helpers.js",
    "npm:*": "third-party/npm/*.js",
    "github:*": "third-party/github/*.js"
  }
});

System.config({
  "map": {
    "npm:event-stream": "npm:event-stream@^3.1.5",
    "nodelibs": "github:jspm/nodelibs@master",
    "npm:event-stream@3.1.5": {
      "through": "npm:through@^2.3.1",
      "pause-stream": "npm:pause-stream@0.0.11",
      "from": "npm:from@0",
      "stream-combiner": "npm:stream-combiner@^0.0.4",
      "map-stream": "npm:map-stream@0.1",
      "duplexer": "npm:duplexer@^0.1.1",
      "split": "npm:split@0.2"
    },
    "npm:stream-combiner@0.0.4": {
      "duplexer": "npm:duplexer@^0.1.1"
    },
    "npm:pause-stream@0.0.11": {
      "through": "npm:through@2.3"
    },
    "npm:from@0.1.3": {},
    "npm:through@2.3.4": {},
    "npm:duplexer@0.1.1": {},
    "npm:split@0.2.10": {
      "through": "npm:through@2"
    },
    "npm:map-stream@0.1.0": {},
    "github:jspm/nodelibs@0.0.2": {
      "base64-js": "npm:base64-js@^0.0.4",
      "ieee754": "npm:ieee754@^1.1.1",
      "inherits": "npm:inherits@^2.0.1",
      "Base64": "npm:Base64@0.2",
      "json": "github:systemjs/plugin-json@master"
    },
    "npm:base64-js@0.0.4": {},
    "npm:Base64@0.2.1": {},
    "npm:inherits@2.0.1": {},
    "npm:ieee754@1.1.3": {},
    "github:jspm/nodelibs@master": {
      "Base64": "npm:Base64@0.2",
      "base64-js": "npm:base64-js@^0.0.4",
      "ieee754": "npm:ieee754@^1.1.1",
      "inherits": "npm:inherits@^2.0.1",
      "json": "github:systemjs/plugin-json@master"
    }
  }
});

System.config({
  "versions": {
    "npm:event-stream": "3.1.5",
    "npm:through": "2.3.4",
    "npm:pause-stream": "0.0.11",
    "npm:from": "0.1.3",
    "npm:stream-combiner": "0.0.4",
    "npm:map-stream": "0.1.0",
    "npm:duplexer": "0.1.1",
    "npm:split": "0.2.10",
    "github:jspm/nodelibs": [
      "master",
      "0.0.2"
    ],
    "npm:base64-js": "0.0.4",
    "npm:ieee754": "1.1.3",
    "npm:inherits": "2.0.1",
    "npm:Base64": "0.2.1",
    "github:systemjs/plugin-json": "master"
  }
});

