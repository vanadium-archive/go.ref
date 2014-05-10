package com.boxes;

import android.util.Log;

public class GoNative {
	static native void registerAsPeer();
	static native void connectPeer();
	static native void sendDrawBox(String boxId, float oX, float oY, float cX, float cY);
	static native void registerAddBox(BoxDrawingController bdc);
	
	static {
		System.loadLibrary("boxesp2p");
	}
}
