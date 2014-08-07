'use strict';

var express = require('express');
var pathlib = require('path');

var app = express();

function pathTo(path) {
  return pathlib.join(__dirname, path);
}

app.use('/public', express.static(pathTo('public')));
app.use('/third_party', express.static(pathTo('third_party')));

var routes = ['/', '/lists/*'];
var handler = function(req, res) {
  res.sendfile('index.html');
};
for (var i = 0; i < routes.length; i++) {
  app.get(routes[i], handler);
}

var server = app.listen(4000, function() {
  console.log('Serving http://localhost:%d', server.address().port);
});
