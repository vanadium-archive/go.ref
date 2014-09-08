var veyron = require('veyron');

veyron.init(function(err, rt){
  if (err) throw err;

  function bindAndSendPing(){
    rt.bindTo('pingpong', function(err, s){
      if (err) throw err;

      s.ping('PING', function(err, pong){
        if (err) throw err;

        console.log(pong);
        process.exit(0);
      });
    });
  }

  // Give the server some time to start.
  // TODO(nlacasse): how are we going to handle this race condition in the
  // actual playground?
  setTimeout(bindAndSendPing, 500);
});
