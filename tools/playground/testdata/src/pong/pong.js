var veyron = require('veyron');
var leafDispatcher = require('veyron/src/ipc/leaf_dispatcher');

var pingPongService = {
  ping: function(msg){
    console.log(msg);
    return 'PONG';
  }
};

veyron.init(function(err, rt) {
  if (err) throw err;

  rt.serve('pingpong', leafDispatcher(pingPongService), function(err) {
    if (err) throw err;
  });
});
