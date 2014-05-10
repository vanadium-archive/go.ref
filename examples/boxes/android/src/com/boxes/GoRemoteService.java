package com.boxes;

import android.graphics.PointF;
import android.os.Handler;
import android.os.HandlerThread;
import android.os.Message;

public class GoRemoteService extends HandlerThread {
	private static final int MESSAGE_DRAW = 0;
	private final Handler mRequestHandler;

	// Handle the send to the peer device in a separate thread.
	public GoRemoteService() {
		super("GoRemoteService");
		mRequestHandler = new Handler() {
			@Override
			public void handleMessage(Message msg) {
				if (msg.what == MESSAGE_DRAW) {
					final Box box = new Box(msg.getData());
					final PointF origin = box.getOrigin();
					final PointF current = box.getCurrent();
					GoNative.sendDrawBox(box.getBoxId(), origin.x, origin.y, current.x, current.y);
				}
			}
		};
	}
	
	// Send a box to a peer device via the go native shared library.
	public void RemoteDraw(Box box) {
		final Message msg = mRequestHandler.obtainMessage(MESSAGE_DRAW);
		msg.setData(box.toBundle());
		msg.sendToTarget();
	}
}
