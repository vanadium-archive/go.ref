var veyron = require('veyron');

veyron.init(function(err, rt){
  if (err) throw err;

  rt.bindTo('pingpong', function(err, s){
    if (err) throw err;

    s.ping('PING', function(err, pong){
      if (err) throw err;

      console.log(pong);
      process.exit(0);
    });
  });
});
