'use strict';

var express = require('express');
var pathlib = require('path');

var app = express();

function pathTo(path) {
  return pathlib.join(__dirname, path);
}

app.use('/public', express.static(pathTo('public')));

app.get('*', function(req, res) {
  res.sendfile('index.html');
});

var server = app.listen(4000, function() {
  console.log('Serving http://localhost:%d', server.address().port);
});
