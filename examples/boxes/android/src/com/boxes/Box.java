package com.boxes;

import java.util.Random;
import java.util.concurrent.atomic.AtomicInteger;

import android.graphics.PointF;
import android.os.Bundle;

/*
 * Box represents a square on a canvas that has been drawn. The
 * ACTION_DOWN event becomes the origin of the box and the final
 * position is when the ACTION_UP event is detected. 
 */
public class Box {
	// Start/End positions of the square box
	private PointF mOrigin;
	private PointF mCurrent;
	
	// Unique name of this box
	private String mBoxId;
	
	// UniqueID that identifies which device a box came from
	// when there are multiple peers.
	private static AtomicInteger idCounter = new AtomicInteger(0);
	private static String uniqueID;
	static {
		uniqueID = "box:" + Math.abs(new Random().nextInt());
	}
	
	// Initialize a new box with a given start co-ordinate.
	public Box(PointF origin) {
		mOrigin = mCurrent = origin;
		mBoxId = uniqueID + "" + idCounter.incrementAndGet();
	}
	
	// Initialize a new box with given attributes
	public Box(String boxId, PointF origin, PointF current) {
		mBoxId = boxId;
		mOrigin = origin;
		mCurrent = current;
	}
	
	// Initialize a box from a packaged bundle. 
	public Box(Bundle b) {
		final float[] points = b.getFloatArray("mPoints");
		setOrigin(new PointF(points[0], points[1]));
		setCurrent(new PointF(points[2], points[3]));
		setBoxId(b.getString("mBoxId"));
	}	

	// Package a box into a bundle that can be stored.
	public Bundle toBundle() {
		final float[] points = { mOrigin.x, mOrigin.y, mCurrent.x, mCurrent.y };
		final Bundle bundle = new Bundle();
		bundle.putFloatArray("mPoints", points);
	    bundle.putString("mBoxId", mBoxId);
	    return bundle;		
	}
 	
	public String getBoxId() {
		return mBoxId;
	}
	
	public void setBoxId(String boxId) {
		mBoxId = boxId;
	}
	
	public void setCurrent(PointF current) {
		mCurrent = current;
	}
	
	public PointF getCurrent() {
		return mCurrent;
	}
	
	public void setOrigin(PointF origin) {
		mOrigin = origin;
	}
	
	public PointF getOrigin() {
		return mOrigin;
	}
}
